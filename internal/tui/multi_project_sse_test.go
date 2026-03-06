package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestModelHasTasksByProjectField verifies Model has tasksByProject field.
func TestModelHasTasksByProjectField(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Test that field exists and is initialized
	if m.tasksByProject == nil {
		t.Fatal("tasksByProject should be initialized")
	}

	// Test that it's empty initially
	if len(m.tasksByProject) != 0 {
		t.Errorf("tasksByProject should be empty initially, got %d entries", len(m.tasksByProject))
	}
}

// TestModelHasSSEClientsField verifies Model has sseClients field.
func TestModelHasSSEClientsField(t *testing.T) {
	// Single-project mode: sseClients should be empty
	cfg1 := Config{
		APIURL:  "http://localhost:3333",
		Project: "single-proj",
	}
	m1 := NewModel(cfg1)

	if m1.sseClients == nil {
		t.Fatal("sseClients should be initialized")
	}

	if len(m1.sseClients) != 0 {
		t.Errorf("sseClients should be empty in single-project mode, got %d entries", len(m1.sseClients))
	}

	// Multi-project mode: sseClients should be populated
	cfg2 := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m2 := NewModel(cfg2)

	if m2.sseClients == nil {
		t.Fatal("sseClients should be initialized")
	}

	if len(m2.sseClients) != 2 {
		t.Errorf("sseClients should have 2 entries in multi-project mode, got %d", len(m2.sseClients))
	}
}

// TestTasksUpdatedMsgHasProjectID verifies TasksUpdatedMsg has ProjectID field.
func TestTasksUpdatedMsgHasProjectID(t *testing.T) {
	msg := TasksUpdatedMsg{
		ProjectID: "test-project",
		Tasks:     []types.ResolvedTask{},
		Stats:     nil,
	}

	if msg.ProjectID != "test-project" {
		t.Errorf("ProjectID field not accessible, expected 'test-project', got '%s'", msg.ProjectID)
	}
}

// TestResolvedTaskHasProjectIDField verifies ResolvedTask type has ProjectID field.
func TestResolvedTaskHasProjectIDField(t *testing.T) {
	task := types.ResolvedTask{
		ID:        "test-id",
		ProjectID: "test-project",
	}

	if task.ProjectID != "test-project" {
		t.Errorf("ProjectID field not accessible, expected 'test-project', got '%s'", task.ProjectID)
	}
}

// TestInitCreatesMultipleSSEClientsForMultiProject verifies Init() creates SSE clients for each project.
func TestInitCreatesMultipleSSEClientsForMultiProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2", "proj3"},
	}
	m := NewModel(cfg)

	// After NewModel(), sseClients should have one entry per project
	if len(m.sseClients) != 3 {
		t.Errorf("Expected 3 SSE clients for 3 projects, got %d", len(m.sseClients))
	}

	// Verify each project has a client
	for _, proj := range cfg.Projects {
		if m.sseClients[proj] == nil {
			t.Errorf("Missing SSE client for project '%s'", proj)
		}
	}
}

// TestInitCreatesSingleSSEClientForSingleProject verifies Init() uses legacy single-client mode for single project.
func TestInitCreatesSingleSSEClientForSingleProject(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "single-proj",
	}
	m := NewModel(cfg)

	// For single-project mode, should use legacy sseClient field
	if m.sseClient == nil {
		t.Error("Expected legacy sseClient to be set for single-project mode")
	}

	// sseClients should be empty in single-project mode
	if len(m.sseClients) != 0 {
		t.Errorf("Expected sseClients to be empty in single-project mode, got %d", len(m.sseClients))
	}
}

// TestTasksUpdatedMsgStoresTasksPerProject verifies Update() stores tasks by project.
func TestTasksUpdatedMsgStoresTasksPerProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Simulate receiving tasks for proj1
	tasks1 := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
		{ID: "task2", Title: "Task 2", ProjectID: "proj1"},
	}
	msg1 := TasksUpdatedMsg{
		ProjectID: "proj1",
		Tasks:     tasks1,
		Stats:     nil,
	}

	updatedModel, _ := m.Update(msg1)
	m = updatedModel.(Model)

	// Verify tasks are stored under proj1
	if len(m.tasksByProject["proj1"]) != 2 {
		t.Errorf("Expected 2 tasks for proj1, got %d", len(m.tasksByProject["proj1"]))
	}

	// Simulate receiving tasks for proj2
	tasks2 := []types.ResolvedTask{
		{ID: "task3", Title: "Task 3", ProjectID: "proj2"},
	}
	msg2 := TasksUpdatedMsg{
		ProjectID: "proj2",
		Tasks:     tasks2,
		Stats:     nil,
	}

	updatedModel2, _ := m.Update(msg2)
	m = updatedModel2.(Model)

	// Verify tasks are stored under proj2
	if len(m.tasksByProject["proj2"]) != 1 {
		t.Errorf("Expected 1 task for proj2, got %d", len(m.tasksByProject["proj2"]))
	}

	// Verify proj1 tasks are still there
	if len(m.tasksByProject["proj1"]) != 2 {
		t.Errorf("Expected proj1 tasks to persist, got %d", len(m.tasksByProject["proj1"]))
	}
}
