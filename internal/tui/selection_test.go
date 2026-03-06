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

	// Render with selection
	view := model.taskTree.ViewWithSelection(80, 10, model.selectedTasks)

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
