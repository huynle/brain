package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestGetAllTasksWithEmptyMap verifies getAllTasks returns empty slice when tasksByProject is empty.
func TestGetAllTasksWithEmptyMap(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// tasksByProject should be empty initially
	result := m.getAllTasks()

	if result == nil {
		t.Fatal("getAllTasks should return empty slice, not nil")
	}

	if len(result) != 0 {
		t.Errorf("getAllTasks should return empty slice for empty tasksByProject, got %d tasks", len(result))
	}
}

// TestGetAllTasksWithSingleProject verifies getAllTasks returns tasks from single project.
func TestGetAllTasksWithSingleProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1"},
	}
	m := NewModel(cfg)

	// Add tasks to proj1
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
		{ID: "task2", Title: "Task 2", ProjectID: "proj1"},
	}

	result := m.getAllTasks()

	if len(result) != 2 {
		t.Errorf("Expected 2 tasks, got %d", len(result))
	}

	// Verify tasks are present (order doesn't matter)
	taskIDs := make(map[string]bool)
	for _, task := range result {
		taskIDs[task.ID] = true
	}

	if !taskIDs["task1"] || !taskIDs["task2"] {
		t.Errorf("Expected task1 and task2 in results, got IDs: %v", taskIDs)
	}
}

// TestGetAllTasksWithMultipleProjects verifies getAllTasks merges tasks from all projects.
func TestGetAllTasksWithMultipleProjects(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2", "proj3"},
	}
	m := NewModel(cfg)

	// Add tasks to multiple projects
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
		{ID: "task2", Title: "Task 2", ProjectID: "proj1"},
	}
	m.tasksByProject["proj2"] = []types.ResolvedTask{
		{ID: "task3", Title: "Task 3", ProjectID: "proj2"},
	}
	m.tasksByProject["proj3"] = []types.ResolvedTask{
		{ID: "task4", Title: "Task 4", ProjectID: "proj3"},
		{ID: "task5", Title: "Task 5", ProjectID: "proj3"},
	}

	result := m.getAllTasks()

	if len(result) != 5 {
		t.Errorf("Expected 5 tasks total, got %d", len(result))
	}

	// Verify all tasks are present
	taskIDs := make(map[string]bool)
	for _, task := range result {
		taskIDs[task.ID] = true
	}

	expectedIDs := []string{"task1", "task2", "task3", "task4", "task5"}
	for _, id := range expectedIDs {
		if !taskIDs[id] {
			t.Errorf("Expected task %s in results, but it was missing", id)
		}
	}
}

// TestGetAllTasksNoTasksLost verifies no tasks are lost during merge.
func TestGetAllTasksNoTasksLost(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"alpha", "beta"},
	}
	m := NewModel(cfg)

	// Add specific tasks with all fields populated
	m.tasksByProject["alpha"] = []types.ResolvedTask{
		{ID: "a1", Title: "Alpha 1", ProjectID: "alpha", Status: "pending"},
		{ID: "a2", Title: "Alpha 2", ProjectID: "alpha", Status: "in_progress"},
	}
	m.tasksByProject["beta"] = []types.ResolvedTask{
		{ID: "b1", Title: "Beta 1", ProjectID: "beta", Status: "completed"},
	}

	result := m.getAllTasks()

	// Count tasks per project in result
	countByProject := make(map[string]int)
	for _, task := range result {
		countByProject[task.ProjectID]++
	}

	if countByProject["alpha"] != 2 {
		t.Errorf("Expected 2 tasks from alpha, got %d", countByProject["alpha"])
	}
	if countByProject["beta"] != 1 {
		t.Errorf("Expected 1 task from beta, got %d", countByProject["beta"])
	}

	// Verify specific task properties are preserved
	for _, task := range result {
		if task.ID == "a1" && task.Status != "pending" {
			t.Errorf("Task a1 status not preserved, expected 'pending', got '%s'", task.Status)
		}
		if task.ID == "b1" && task.Title != "Beta 1" {
			t.Errorf("Task b1 title not preserved, expected 'Beta 1', got '%s'", task.Title)
		}
	}
}

// ============================================================================
// Tests for syncActiveProjectView()
// ============================================================================

// TestSyncActiveProjectViewSingleProjectMode verifies syncActiveProjectView is no-op in single-project mode.
func TestSyncActiveProjectViewSingleProjectMode(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "single-proj",
	}
	m := NewModel(cfg)

	// Add some tasks to legacy field
	m.tasks = []types.ResolvedTask{
		{ID: "task1", Title: "Task 1"},
	}

	// Call syncActiveProjectView
	m.syncActiveProjectView()

	// In single-project mode, tasks should remain unchanged
	if len(m.tasks) != 1 {
		t.Errorf("Expected tasks to remain unchanged in single-project mode, got %d tasks", len(m.tasks))
	}
}

// TestSyncActiveProjectViewAggregateMode verifies syncActiveProjectView shows all tasks when activeProjectID="all".
func TestSyncActiveProjectViewAggregateMode(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Add tasks to multiple projects
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
	}
	m.tasksByProject["proj2"] = []types.ResolvedTask{
		{ID: "task2", Title: "Task 2", ProjectID: "proj2"},
	}

	// Set activeProjectID to "all"
	m.activeProjectID = "all"

	// Call syncActiveProjectView
	m.syncActiveProjectView()

	// Should merge all tasks
	if len(m.tasks) != 2 {
		t.Errorf("Expected 2 tasks in aggregate view, got %d", len(m.tasks))
	}

	// Verify taskTree was updated
	// Note: We can't easily test taskTree internals, but we can verify tasks were set
	// This is tested indirectly by checking m.tasks
}

// TestSyncActiveProjectViewSpecificProject verifies syncActiveProjectView shows only specific project tasks.
func TestSyncActiveProjectViewSpecificProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Add tasks to multiple projects
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
		{ID: "task2", Title: "Task 2", ProjectID: "proj1"},
	}
	m.tasksByProject["proj2"] = []types.ResolvedTask{
		{ID: "task3", Title: "Task 3", ProjectID: "proj2"},
	}

	// Set activeProjectID to "proj1"
	m.activeProjectID = "proj1"

	// Call syncActiveProjectView
	m.syncActiveProjectView()

	// Should show only proj1 tasks
	if len(m.tasks) != 2 {
		t.Errorf("Expected 2 tasks for proj1, got %d", len(m.tasks))
	}

	// Verify all tasks are from proj1
	for _, task := range m.tasks {
		if task.ProjectID != "proj1" {
			t.Errorf("Expected all tasks from proj1, got task from %s", task.ProjectID)
		}
	}
}

// TestSyncActiveProjectViewWithFilter verifies filter is preserved after sync.
func TestSyncActiveProjectViewWithFilter(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Add tasks
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Important Task", ProjectID: "proj1"},
	}
	m.tasksByProject["proj2"] = []types.ResolvedTask{
		{ID: "task2", Title: "Other Task", ProjectID: "proj2"},
	}

	// Set active filter
	m.filterQuery = "Important"
	m.filterActive = true
	m.activeProjectID = "all"

	// Call syncActiveProjectView
	m.syncActiveProjectView()

	// Filter should still be active
	if !m.filterActive {
		t.Error("Expected filter to remain active after sync")
	}

	// Note: We can't easily verify the filtered task list without accessing taskTree internals
	// The important part is that filterActive remains true, which signals that applyFilter() was called
}

// TestSyncActiveProjectViewSwitchingProjects verifies view updates when switching between projects.
func TestSyncActiveProjectViewSwitchingProjects(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Add different task counts to projects
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
	}
	m.tasksByProject["proj2"] = []types.ResolvedTask{
		{ID: "task2", Title: "Task 2", ProjectID: "proj2"},
		{ID: "task3", Title: "Task 3", ProjectID: "proj2"},
	}

	// Start with proj1
	m.activeProjectID = "proj1"
	m.syncActiveProjectView()

	if len(m.tasks) != 1 {
		t.Errorf("Expected 1 task for proj1, got %d", len(m.tasks))
	}

	// Switch to proj2
	m.activeProjectID = "proj2"
	m.syncActiveProjectView()

	if len(m.tasks) != 2 {
		t.Errorf("Expected 2 tasks for proj2, got %d", len(m.tasks))
	}

	// Switch to "all"
	m.activeProjectID = "all"
	m.syncActiveProjectView()

	if len(m.tasks) != 3 {
		t.Errorf("Expected 3 tasks for all projects, got %d", len(m.tasks))
	}
}

// ============================================================================
// Tests for Update() integration with syncActiveProjectView()
// ============================================================================

// TestUpdateTasksUpdatedMsgCallsSyncActiveProjectView verifies Update() calls syncActiveProjectView() for TasksUpdatedMsg.
func TestUpdateTasksUpdatedMsgCallsSyncActiveProjectView(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.activeProjectID = "all"

	// Simulate receiving TasksUpdatedMsg for proj1
	msg := TasksUpdatedMsg{
		ProjectID: "proj1",
		Tasks: []types.ResolvedTask{
			{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
		},
		Stats: nil,
	}

	updatedModel, _ := m.Update(msg)
	m = updatedModel.(Model)

	// Verify tasks were stored in tasksByProject
	if len(m.tasksByProject["proj1"]) != 1 {
		t.Errorf("Expected 1 task in tasksByProject[proj1], got %d", len(m.tasksByProject["proj1"]))
	}

	// In aggregate view ("all"), m.tasks should have tasks from all projects
	// Since only proj1 has tasks, m.tasks should have 1 task
	if len(m.tasks) != 1 {
		t.Errorf("Expected syncActiveProjectView to set m.tasks to 1 task, got %d", len(m.tasks))
	}
}

// TestUpdateTasksUpdatedMsgMultipleProjects verifies Update() handles multiple projects correctly.
func TestUpdateTasksUpdatedMsgMultipleProjects(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.activeProjectID = "all"

	// Send tasks for proj1
	msg1 := TasksUpdatedMsg{
		ProjectID: "proj1",
		Tasks: []types.ResolvedTask{
			{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
		},
		Stats: nil,
	}
	updatedModel1, _ := m.Update(msg1)
	m = updatedModel1.(Model)

	// Should have 1 task total
	if len(m.tasks) != 1 {
		t.Errorf("After proj1 update, expected 1 task total, got %d", len(m.tasks))
	}

	// Send tasks for proj2
	msg2 := TasksUpdatedMsg{
		ProjectID: "proj2",
		Tasks: []types.ResolvedTask{
			{ID: "task2", Title: "Task 2", ProjectID: "proj2"},
			{ID: "task3", Title: "Task 3", ProjectID: "proj2"},
		},
		Stats: nil,
	}
	updatedModel2, _ := m.Update(msg2)
	m = updatedModel2.(Model)

	// Should have 3 tasks total (1 from proj1 + 2 from proj2)
	if len(m.tasks) != 3 {
		t.Errorf("After proj2 update, expected 3 tasks total, got %d", len(m.tasks))
	}

	// Verify tasksByProject has both projects
	if len(m.tasksByProject["proj1"]) != 1 {
		t.Errorf("Expected proj1 to have 1 task, got %d", len(m.tasksByProject["proj1"]))
	}
	if len(m.tasksByProject["proj2"]) != 2 {
		t.Errorf("Expected proj2 to have 2 tasks, got %d", len(m.tasksByProject["proj2"]))
	}
}

// TestUpdateTasksUpdatedMsgSpecificProjectView verifies Update() respects specific project view.
func TestUpdateTasksUpdatedMsgSpecificProjectView(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.activeProjectID = "proj1" // Viewing only proj1

	// Send tasks for proj1
	msg1 := TasksUpdatedMsg{
		ProjectID: "proj1",
		Tasks: []types.ResolvedTask{
			{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
		},
		Stats: nil,
	}
	updatedModel1, _ := m.Update(msg1)
	m = updatedModel1.(Model)

	// Should show only proj1 tasks
	if len(m.tasks) != 1 {
		t.Errorf("Expected 1 task for proj1 view, got %d", len(m.tasks))
	}

	// Send tasks for proj2
	msg2 := TasksUpdatedMsg{
		ProjectID: "proj2",
		Tasks: []types.ResolvedTask{
			{ID: "task2", Title: "Task 2", ProjectID: "proj2"},
		},
		Stats: nil,
	}
	updatedModel2, _ := m.Update(msg2)
	m = updatedModel2.(Model)

	// Should still show only proj1 tasks (view hasn't changed)
	if len(m.tasks) != 1 {
		t.Errorf("Expected proj1 view to remain showing 1 task, got %d", len(m.tasks))
	}

	// Verify proj2 tasks were stored in tasksByProject
	if len(m.tasksByProject["proj2"]) != 1 {
		t.Errorf("Expected proj2 tasks to be stored, got %d", len(m.tasksByProject["proj2"]))
	}
}
