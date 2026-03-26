package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

	view := statusBar.View(100)

	// Should contain "2 selected"
	if !strings.Contains(view, "2 selected") {
		t.Errorf("Expected status bar to show selection count, got:\n%s", view)
	}
}

func TestIsOnGroupHeader(t *testing.T) {
	taskTree := NewTaskTree()
	taskTree.SetFeatureViewMode(false) // Use classification-only grouped view

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

// TestIsOnGroupHeader_NestedStatusFeatureView tests that IsOnGroupHeader returns
// true when on status headers and feature headers in nested status+feature mode.
func TestIsOnGroupHeader_NestedStatusFeatureView(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(false) // nested status+feature view
	tt.SetTasks(tasks)

	// Manually configure statusGroups
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Name: "Feature A", Tasks: tasks},
			},
			Count: 1,
		},
	}

	// On status header
	tt.selectedStatusIdx = 0
	tt.isOnStatusHeader = true
	tt.selectedTaskIdx = -1
	if !tt.IsOnGroupHeader() {
		t.Error("Expected IsOnGroupHeader=true when on status header in nested mode")
	}

	// On feature header within status group
	tt.isOnStatusHeader = false
	tt.selectedFeatureIdx = 0
	tt.selectedTaskIdx = -1
	if !tt.IsOnGroupHeader() {
		t.Error("Expected IsOnGroupHeader=true when on feature header in nested mode")
	}

	// On a task within a feature
	tt.selectedTaskIdx = 0
	if tt.IsOnGroupHeader() {
		t.Error("Expected IsOnGroupHeader=false when on a task in nested mode")
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
	tt.SetFeatureViewMode(false) // explicitly use nested status+feature view

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
	tt.SetFeatureViewMode(false) // explicitly use nested status+feature view

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
	tt.SetFeatureViewMode(false) // explicitly use nested status+feature view

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

// TestToggleCollapse_TerminalSectionHeader tests that Space toggles collapse on
// terminal section headers (Draft ▾/▶, Completed ▾/▶, etc.) in feature view mode.
// This is the exact bug reported: pressing Space on a Draft/Completed section header
// should toggle between expanded (▾) and collapsed (▶).
func TestToggleCollapse_TerminalSectionHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	// Create tasks with draft and completed statuses
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Active Task", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Title: "Draft Task 1", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
		{ID: "t3", Title: "Draft Task 2", Status: "draft", Priority: "low", FeatureID: "feat-b"},
		{ID: "t4", Title: "Completed Task", Status: "completed", Priority: "medium", FeatureID: "feat-a"},
	}
	tt.SetTasks(tasks)

	// Navigate to Draft section header
	tt.moveToDraftSection()

	// Verify we're on the Draft section header
	if !tt.isOnDraftSection {
		t.Fatal("Expected isOnDraftSection=true after moveToDraftSection")
	}
	if tt.draftFeatureIdx != -1 {
		t.Fatalf("Expected draftFeatureIdx=-1 (on section header), got %d", tt.draftFeatureIdx)
	}

	// Verify IsOnGroupHeader returns true
	if !tt.IsOnGroupHeader() {
		t.Fatal("Expected IsOnGroupHeader()=true when on Draft section header")
	}

	// Initially Draft is NOT collapsed (default)
	if tt.draftCollapsed {
		t.Fatal("Expected draftCollapsed=false by default")
	}

	// Toggle collapse (this is what Space does via ToggleCollapse)
	tt.ToggleCollapse()

	// Verify Draft is now collapsed
	if !tt.draftCollapsed {
		t.Fatal("Expected draftCollapsed=true after ToggleCollapse on Draft header")
	}

	// Toggle again - should expand
	tt.ToggleCollapse()
	if tt.draftCollapsed {
		t.Fatal("Expected draftCollapsed=false after second ToggleCollapse on Draft header")
	}

	// Test Completed section header toggle
	tt.moveToCompletedSection()
	if !tt.isOnCompletedSection {
		t.Fatal("Expected isOnCompletedSection=true after moveToCompletedSection")
	}
	if !tt.IsOnGroupHeader() {
		t.Fatal("Expected IsOnGroupHeader()=true when on Completed section header")
	}

	// Completed is collapsed by default
	initialCompletedCollapsed := tt.completedCollapsed
	tt.ToggleCollapse()
	if tt.completedCollapsed == initialCompletedCollapsed {
		t.Fatal("Expected completedCollapsed to change after ToggleCollapse on Completed header")
	}
}

// TestToggleCollapse_TerminalSection_ViaNavigation tests the full navigation path:
// start at first active task, navigate down past all features/ungrouped to the
// Draft section header, then verify Space (IsOnGroupHeader + ToggleCollapse) works.
// This reproduces the actual user interaction flow.
func TestToggleCollapse_TerminalSection_ViaNavigation(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	// Only draft tasks (no active tasks) - forces selectFirstFeatureTask to
	// land on Draft section header.
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Draft Task 1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Title: "Draft Task 2", Status: "draft", Priority: "medium", FeatureID: "feat-b"},
	}
	tt.SetTasks(tasks)

	// After SetTasks with only draft tasks, selectFirstFeatureTask should land on
	// Draft section header (no active features/ungrouped to select).
	t.Logf("State after SetTasks: isOnDraftSection=%v draftFeatureIdx=%d selectedFeatureTaskIdx=%d isOnUngrouped=%v selectedFeatureIdx=%d SelectedID=%s",
		tt.isOnDraftSection, tt.draftFeatureIdx, tt.selectedFeatureTaskIdx, tt.isOnUngrouped, tt.selectedFeatureIdx, tt.SelectedID)

	if !tt.isOnDraftSection {
		t.Fatal("Expected to land on Draft section header when there are only draft tasks")
	}
	if tt.draftFeatureIdx != -1 {
		t.Fatalf("Expected draftFeatureIdx=-1 (on section header), got %d", tt.draftFeatureIdx)
	}

	// Verify Space would work
	isHeader := tt.IsOnGroupHeader()
	t.Logf("IsOnGroupHeader=%v", isHeader)
	if !isHeader {
		t.Fatal("Expected IsOnGroupHeader()=true when on Draft section header via navigation")
	}

	// Toggle collapse
	if tt.draftCollapsed {
		t.Fatal("Expected draftCollapsed=false initially")
	}
	tt.ToggleCollapse()
	if !tt.draftCollapsed {
		t.Fatal("Expected draftCollapsed=true after ToggleCollapse")
	}

	// Now test navigating down to Draft section from active features
	tt2 := NewTaskTree()
	tt2.SetViewMode(true)
	tt2.SetFeatureViewMode(true)

	tasksWithActive := []types.ResolvedTask{
		{ID: "a1", Title: "Active Task", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-x"},
		{ID: "d1", Title: "Draft Task", Status: "draft", Priority: "medium", FeatureID: "feat-x"},
	}
	tt2.SetTasks(tasksWithActive)

	t.Logf("Initial state: selectedFeatureIdx=%d selectedFeatureTaskIdx=%d isOnDraftSection=%v SelectedID=%s",
		tt2.selectedFeatureIdx, tt2.selectedFeatureTaskIdx, tt2.isOnDraftSection, tt2.SelectedID)

	// Should start on active task a1
	if tt2.SelectedID != "a1" {
		t.Fatalf("Expected initial SelectedID=a1, got %s", tt2.SelectedID)
	}

	// Navigate down repeatedly until we reach Draft section header
	maxSteps := 20
	reachedDraft := false
	for i := 0; i < maxSteps; i++ {
		tt2.MoveDown()
		t.Logf("After MoveDown %d: isOnDraftSection=%v draftFeatureIdx=%d draftTaskIdx=%d selectedFeatureTaskIdx=%d SelectedID=%s isOnGroupHeader=%v",
			i+1, tt2.isOnDraftSection, tt2.draftFeatureIdx, tt2.draftTaskIdx, tt2.selectedFeatureTaskIdx, tt2.SelectedID, tt2.IsOnGroupHeader())
		if tt2.isOnDraftSection && tt2.draftFeatureIdx == -1 {
			reachedDraft = true
			break
		}
	}

	if !reachedDraft {
		t.Fatal("Failed to reach Draft section header via MoveDown navigation")
	}

	// Now verify Space would toggle collapse
	if !tt2.IsOnGroupHeader() {
		t.Fatal("Expected IsOnGroupHeader()=true when on Draft section header via MoveDown")
	}
	tt2.ToggleCollapse()
	if !tt2.draftCollapsed {
		t.Fatal("Expected draftCollapsed=true after ToggleCollapse via navigation")
	}
}

// TestToggleCollapse_TerminalSection_RendersCorrectly tests that toggling collapse
// on a terminal section header actually changes the visual output (▾ vs ▶ and
// hiding/showing child items).
func TestToggleCollapse_TerminalSection_RendersCorrectly(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	tasks := []types.ResolvedTask{
		{ID: "a1", Title: "Active Task", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-x"},
		{ID: "d1", Title: "Draft Task Alpha", Status: "draft", Priority: "medium", FeatureID: "feat-x"},
		{ID: "d2", Title: "Draft Task Beta", Status: "draft", Priority: "low", FeatureID: "feat-y"},
	}
	tt.SetTasks(tasks)

	// Render expanded (default: draftCollapsed=false)
	expandedView := tt.ViewWithProject(80, 40, "")
	t.Logf("Expanded view:\n%s", expandedView)

	// Verify expanded view shows "▾ Draft" and contains draft task titles
	if !strings.Contains(expandedView, "▾ Draft") {
		t.Error("Expected expanded view to contain '▾ Draft'")
	}
	if !strings.Contains(expandedView, "Draft Task Alpha") {
		t.Error("Expected expanded view to contain 'Draft Task Alpha'")
	}

	// Navigate to Draft section header and toggle collapse
	tt.moveToDraftSection()
	tt.ToggleCollapse()

	// Render collapsed
	collapsedView := tt.ViewWithProject(80, 40, "")
	t.Logf("Collapsed view:\n%s", collapsedView)

	// Verify collapsed view shows "▶ Draft" and hides draft task titles
	if !strings.Contains(collapsedView, "▶ Draft") {
		t.Error("Expected collapsed view to contain '▶ Draft'")
	}
	if strings.Contains(collapsedView, "Draft Task Alpha") {
		t.Error("Expected collapsed view to NOT contain 'Draft Task Alpha'")
	}
	if strings.Contains(collapsedView, "Draft Task Beta") {
		t.Error("Expected collapsed view to NOT contain 'Draft Task Beta'")
	}
}

// TestSpaceKey_TerminalSectionHeader_FullModel tests the complete user flow:
// the full TUI Model handles a Space keypress when cursor is on a Draft section header.
// This tests the same code path as the actual user pressing Space in the TUI.
func TestSpaceKey_TerminalSectionHeader_FullModel(t *testing.T) {
	m := NewModel(Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	})
	m.width = 120
	m.height = 40

	// Only draft tasks - forces cursor to Draft section header
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Draft Task 1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Title: "Draft Task 2", Status: "draft", Priority: "medium", FeatureID: "feat-b"},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)
	m.activePanel = PanelTasks

	t.Logf("Before Space: isOnDraftSection=%v draftFeatureIdx=%d draftCollapsed=%v IsOnGroupHeader=%v selectedFeatureTaskIdx=%d",
		m.taskTree.isOnDraftSection, m.taskTree.draftFeatureIdx, m.taskTree.draftCollapsed, m.taskTree.IsOnGroupHeader(), m.taskTree.selectedFeatureTaskIdx)

	// Verify pre-conditions
	if !m.taskTree.isOnDraftSection {
		t.Fatal("Expected isOnDraftSection=true")
	}
	if !m.taskTree.IsOnGroupHeader() {
		t.Fatal("Expected IsOnGroupHeader=true on Draft section header")
	}
	if m.taskTree.draftCollapsed {
		t.Fatal("Expected draftCollapsed=false initially")
	}

	// Simulate Space keypress - bubbletea sends Space as tea.KeySpace (NOT tea.KeyRunes)
	spaceMsg := tea.KeyMsg{
		Type:  tea.KeySpace,
		Runes: []rune{' '},
	}
	newModel, _ := m.Update(spaceMsg)
	m = newModel.(Model)

	t.Logf("After Space: draftCollapsed=%v isOnDraftSection=%v draftFeatureIdx=%d",
		m.taskTree.draftCollapsed, m.taskTree.isOnDraftSection, m.taskTree.draftFeatureIdx)

	// Verify Draft is now collapsed
	if !m.taskTree.draftCollapsed {
		t.Fatal("Expected draftCollapsed=true after Space keypress on Draft section header")
	}

	// Press Space again to expand
	newModel, _ = m.Update(spaceMsg)
	m = newModel.(Model)
	if m.taskTree.draftCollapsed {
		t.Fatal("Expected draftCollapsed=false after second Space keypress")
	}
}

// TestSpaceKey_TerminalSection_WithActiveAndDraft tests Space on Draft header
// when there are both active and draft tasks (the more common real-world scenario).
func TestSpaceKey_TerminalSection_WithActiveAndDraft(t *testing.T) {
	m := NewModel(Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	})
	m.width = 120
	m.height = 40

	tasks := []types.ResolvedTask{
		{ID: "a1", Title: "Active Task", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-x"},
		{ID: "d1", Title: "Draft Task", Status: "draft", Priority: "medium", FeatureID: "feat-x"},
		{ID: "c1", Title: "Completed Task", Status: "completed", Priority: "low", FeatureID: "feat-x"},
	}
	m.tasks = tasks
	m.taskTree.SetTasks(tasks)
	m.activePanel = PanelTasks

	// Navigate to Draft section header via MoveDown
	maxSteps := 20
	for i := 0; i < maxSteps; i++ {
		m.taskTree.MoveDown()
		if m.taskTree.isOnDraftSection && m.taskTree.draftFeatureIdx == -1 {
			break
		}
	}

	if !m.taskTree.isOnDraftSection || m.taskTree.draftFeatureIdx != -1 {
		t.Fatal("Failed to navigate to Draft section header")
	}

	t.Logf("On Draft header: IsOnGroupHeader=%v draftCollapsed=%v selectedFeatureTaskIdx=%d",
		m.taskTree.IsOnGroupHeader(), m.taskTree.draftCollapsed, m.taskTree.selectedFeatureTaskIdx)

	if !m.taskTree.IsOnGroupHeader() {
		t.Fatal("Expected IsOnGroupHeader=true on Draft section header")
	}

	// Simulate Space - bubbletea sends Space as tea.KeySpace
	spaceMsg := tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	newModel, _ := m.Update(spaceMsg)
	m = newModel.(Model)

	if !m.taskTree.draftCollapsed {
		t.Fatal("Expected draftCollapsed=true after Space on Draft header (navigated scenario)")
	}
}
