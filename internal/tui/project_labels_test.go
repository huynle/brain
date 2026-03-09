package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestRenderGroupedTaskLine_AggregateView_ShowsProjectLabel verifies that
// project labels are rendered when activeProjectID == "all".
func TestRenderGroupedTaskLine_AggregateView_ShowsProjectLabel(t *testing.T) {
	tt := NewTaskTree()

	task := types.ResolvedTask{
		ID:             "task1",
		Title:          "Fix authentication bug",
		ProjectID:      "brain-api",
		Classification: "ready",
		Priority:       "high",
	}

	// Create a mock Model with activeProjectID="all"
	m := Model{
		activeProjectID: "all",
	}

	// Render task line in aggregate view context
	line := tt.renderGroupedTaskLineWithProject(task, false, make(map[string]bool), false, m.activeProjectID, 120)

	// Should contain project label [brain-api]
	if !strings.Contains(line, "[brain-api]") {
		t.Errorf("Expected project label [brain-api] in line, got: %s", line)
	}

	// Should contain task title
	if !strings.Contains(line, "Fix authentication bug") {
		t.Errorf("Expected task title in line, got: %s", line)
	}
}

// TestRenderGroupedTaskLine_SingleProjectView_NoProjectLabel verifies that
// project labels are NOT rendered when activeProjectID is a specific project.
func TestRenderGroupedTaskLine_SingleProjectView_NoProjectLabel(t *testing.T) {
	tt := NewTaskTree()

	task := types.ResolvedTask{
		ID:             "task1",
		Title:          "Fix authentication bug",
		ProjectID:      "brain-api",
		Classification: "ready",
		Priority:       "high",
	}

	// Create a mock Model with specific project
	m := Model{
		activeProjectID: "brain-api",
	}

	// Render task line in single-project view
	line := tt.renderGroupedTaskLineWithProject(task, false, make(map[string]bool), false, m.activeProjectID, 120)

	// Should NOT contain project label
	if strings.Contains(line, "[brain-api]") {
		t.Errorf("Expected NO project label in single-project view, got: %s", line)
	}

	// Should still contain task title
	if !strings.Contains(line, "Fix authentication bug") {
		t.Errorf("Expected task title in line, got: %s", line)
	}
}

// TestRenderGroupedTaskLine_ProjectLabelStyling verifies that project labels
// use the correct styling (cyan, bold).
func TestRenderGroupedTaskLine_ProjectLabelStyling(t *testing.T) {
	tt := NewTaskTree()

	task := types.ResolvedTask{
		ID:             "task1",
		Title:          "Update documentation",
		ProjectID:      "opencode",
		Classification: "ready",
		Priority:       "medium",
	}

	// Render in aggregate view
	line := tt.renderGroupedTaskLineWithProject(task, false, make(map[string]bool), false, "all", 120)

	// Project label should be present
	if !strings.Contains(line, "[opencode]") {
		t.Errorf("Expected project label [opencode] in line, got: %s", line)
	}

	// Note: We can't easily test lipgloss styling in unit tests without rendering,
	// but we can verify the label is present in the correct position
}

// TestRenderGroupedTaskLine_EmptyProjectID_NoLabel verifies that tasks with
// empty ProjectID don't show a label (even in aggregate view).
func TestRenderGroupedTaskLine_EmptyProjectID_NoLabel(t *testing.T) {
	tt := NewTaskTree()

	task := types.ResolvedTask{
		ID:             "task1",
		Title:          "Task with no project",
		ProjectID:      "", // Empty project ID
		Classification: "ready",
		Priority:       "low",
	}

	// Render in aggregate view
	line := tt.renderGroupedTaskLineWithProject(task, false, make(map[string]bool), false, "all", 120)

	// Should NOT contain empty brackets
	if strings.Contains(line, "[]") {
		t.Errorf("Expected NO empty brackets for empty ProjectID, got: %s", line)
	}

	// Should contain task title
	if !strings.Contains(line, "Task with no project") {
		t.Errorf("Expected task title in line, got: %s", line)
	}
}

// TestRenderGroupedTaskLine_WithMultiSelect_ShowsLabelAndCheckbox verifies that
// project labels work correctly with multi-select mode.
func TestRenderGroupedTaskLine_WithMultiSelect_ShowsLabelAndCheckbox(t *testing.T) {
	tt := NewTaskTree()

	task := types.ResolvedTask{
		ID:             "task1",
		Title:          "Add dark mode",
		ProjectID:      "my-project",
		Classification: "ready",
		Priority:       "medium",
	}

	selectedTasks := map[string]bool{
		"task1": true, // This task is selected
	}

	// Render in aggregate view with multi-select
	line := tt.renderGroupedTaskLineWithProject(task, false, selectedTasks, true, "all", 120)

	// Should contain checkbox
	if !strings.Contains(line, "[x]") && !strings.Contains(line, "[ ]") {
		t.Errorf("Expected checkbox in multi-select mode, got: %s", line)
	}

	// Should contain project label
	if !strings.Contains(line, "[my-project]") {
		t.Errorf("Expected project label [my-project] in line, got: %s", line)
	}

	// Should contain task title
	if !strings.Contains(line, "Add dark mode") {
		t.Errorf("Expected task title in line, got: %s", line)
	}
}

// TestRenderGroupedTaskLine_SelectedTask_ShowsLabelAndHighlight verifies that
// project labels work correctly with task selection.
func TestRenderGroupedTaskLine_SelectedTask_ShowsLabelAndHighlight(t *testing.T) {
	tt := NewTaskTree()

	task := types.ResolvedTask{
		ID:             "task1",
		Title:          "Implement feature",
		ProjectID:      "test-project",
		Classification: "ready",
		Priority:       "high",
	}

	// Render in aggregate view with task selected (isSelected=true)
	line := tt.renderGroupedTaskLineWithProject(task, true, make(map[string]bool), false, "all", 120)

	// Should contain project label
	if !strings.Contains(line, "[test-project]") {
		t.Errorf("Expected project label [test-project] in line, got: %s", line)
	}

	// Should contain task title
	if !strings.Contains(line, "Implement feature") {
		t.Errorf("Expected task title in line, got: %s", line)
	}

	// Should have selection marker
	if !strings.Contains(line, "▸") {
		t.Errorf("Expected selection marker in line, got: %s", line)
	}
}

// TestViewGrouped_AggregateView_AllTasksShowProjectLabels verifies that
// all tasks in a grouped view show project labels when in aggregate mode.
func TestViewGrouped_AggregateView_AllTasksShowProjectLabels(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Set up aggregate view
	m.activeProjectID = "all"
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Task from proj1", ProjectID: "proj1", Classification: "ready", Priority: "high"},
	}
	m.tasksByProject["proj2"] = []types.ResolvedTask{
		{ID: "task2", Title: "Task from proj2", ProjectID: "proj2", Classification: "waiting", Priority: "medium"},
	}

	// Sync to populate task tree
	m.syncActiveProjectView()

	// Render the task tree with activeProjectID
	view := m.taskTree.viewGrouped(80, 20, m.activeProjectID)

	// Should contain both project labels
	if !strings.Contains(view, "[proj1]") {
		t.Errorf("Expected [proj1] label in aggregate view, got: %s", view)
	}
	if !strings.Contains(view, "[proj2]") {
		t.Errorf("Expected [proj2] label in aggregate view, got: %s", view)
	}

	// Should contain task titles
	if !strings.Contains(view, "Task from proj1") {
		t.Errorf("Expected 'Task from proj1' in view, got: %s", view)
	}
	if !strings.Contains(view, "Task from proj2") {
		t.Errorf("Expected 'Task from proj2' in view, got: %s", view)
	}
}

// TestViewGrouped_SingleProjectView_NoProjectLabels verifies that
// when viewing a single project, no project labels are shown.
func TestViewGrouped_SingleProjectView_NoProjectLabels(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Set up single-project view
	m.activeProjectID = "proj1"
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Task from proj1", ProjectID: "proj1", Classification: "ready", Priority: "high"},
		{ID: "task2", Title: "Another task", ProjectID: "proj1", Classification: "waiting", Priority: "medium"},
	}

	// Sync to populate task tree
	m.syncActiveProjectView()

	// Render the task tree with activeProjectID
	view := m.taskTree.viewGrouped(80, 20, m.activeProjectID)

	// Should NOT contain project labels
	if strings.Contains(view, "[proj1]") {
		t.Errorf("Expected NO [proj1] label in single-project view, got: %s", view)
	}

	// Should still contain task titles
	if !strings.Contains(view, "Task from proj1") {
		t.Errorf("Expected 'Task from proj1' in view, got: %s", view)
	}
	if !strings.Contains(view, "Another task") {
		t.Errorf("Expected 'Another task' in view, got: %s", view)
	}
}
