package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/types"
)

// ============================================================================
// Phase 3: Tab Navigation Tests
// ============================================================================

// TestUpdate_HKey_PrevTab verifies 'h' navigates to previous tab in multi-project mode.
func TestUpdate_HKey_PrevTab(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Start on tab index 1 (proj1)
	m.projectTabs.ActiveIndex = 1

	// Press 'h'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should move to tab 0 (All)
	if model.projectTabs.ActiveIndex != 0 {
		t.Errorf("Expected tab index 0 after 'h', got %d", model.projectTabs.ActiveIndex)
	}
}

// TestUpdate_LKey_NextTab verifies 'l' navigates to next tab in multi-project mode.
func TestUpdate_LKey_NextTab(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Start on tab index 0 (All)
	m.projectTabs.ActiveIndex = 0

	// Press 'l'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should move to tab 1 (proj1)
	if model.projectTabs.ActiveIndex != 1 {
		t.Errorf("Expected tab index 1 after 'l', got %d", model.projectTabs.ActiveIndex)
	}
}

// TestUpdate_LeftBracket_PrevTab verifies '[' navigates to previous tab.
func TestUpdate_LeftBracket_PrevTab(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.projectTabs.ActiveIndex = 1

	// Press '['
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should move to tab 0
	if model.projectTabs.ActiveIndex != 0 {
		t.Errorf("Expected tab index 0 after '[', got %d", model.projectTabs.ActiveIndex)
	}
}

// TestUpdate_RightBracket_NextTab verifies ']' navigates to next tab.
func TestUpdate_RightBracket_NextTab(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.projectTabs.ActiveIndex = 0

	// Press ']'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should move to tab 1
	if model.projectTabs.ActiveIndex != 1 {
		t.Errorf("Expected tab index 1 after ']', got %d", model.projectTabs.ActiveIndex)
	}
}

// TestUpdate_NumberKey_JumpToTab verifies '1'-'9' jump to specific tabs.
func TestUpdate_NumberKey_JumpToTab(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2", "proj3"},
	}
	m := NewModel(cfg)
	m.projectTabs.ActiveIndex = 0

	// Press '2' to jump to tab 2 (index 1, proj1)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should move to tab index 1
	if model.projectTabs.ActiveIndex != 1 {
		t.Errorf("Expected tab index 1 after '2', got %d", model.projectTabs.ActiveIndex)
	}

	// Press '1' to jump to tab 1 (index 0, All)
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}}
	updated, _ = model.Update(msg)
	model = updated.(Model)

	// Should move to tab index 0
	if model.projectTabs.ActiveIndex != 0 {
		t.Errorf("Expected tab index 0 after '1', got %d", model.projectTabs.ActiveIndex)
	}
}

// TestUpdate_TabNavigation_SyncsTasks verifies tab navigation updates task view.
func TestUpdate_TabNavigation_SyncsTasks(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Add tasks to projects
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
	}
	m.tasksByProject["proj2"] = []types.ResolvedTask{
		{ID: "task2", Title: "Task 2", ProjectID: "proj2"},
		{ID: "task3", Title: "Task 3", ProjectID: "proj2"},
	}

	// Start on tab 0 (All) - should show 3 tasks
	m.projectTabs.ActiveIndex = 0
	m.syncActiveProjectView()

	if len(m.tasks) != 3 {
		t.Fatalf("Expected 3 tasks in 'all' view, got %d", len(m.tasks))
	}

	// Press 'l' to switch to proj1
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should show only proj1 tasks (1 task)
	if len(model.tasks) != 1 {
		t.Errorf("Expected 1 task for proj1, got %d", len(model.tasks))
	}

	// Press 'l' again to switch to proj2
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	updated, _ = model.Update(msg)
	model = updated.(Model)

	// Should show only proj2 tasks (2 tasks)
	if len(model.tasks) != 2 {
		t.Errorf("Expected 2 tasks for proj2, got %d", len(model.tasks))
	}
}

// TestUpdate_TabNavigation_SingleProjectMode verifies no tab switching in single-project mode.
func TestUpdate_TabNavigation_SingleProjectMode(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "single-proj",
	}
	m := NewModel(cfg)

	// In single-project mode, projectTabs should not be used
	// Press 'h' should not cause errors (should be ignored)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	_, _ = m.Update(msg)

	// No panic = success (tab navigation ignored in single-project mode)
}

// TestView_ShowsProjectTabs_MultiProject verifies tabs are rendered in multi-project mode.
func TestView_ShowsProjectTabs_MultiProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Add some tasks to create stats
	m.tasksByProject["proj1"] = []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready"},
	}

	// Update ProjectTabs stats
	m.projectTabs.UpdateStats("proj1", TaskStats{Ready: 1})

	view := m.View()

	// Should contain "All" tab
	if !strings.Contains(view, "All") {
		t.Error("Expected view to contain 'All' tab in multi-project mode")
	}

	// Should contain project names
	if !strings.Contains(view, "proj1") || !strings.Contains(view, "proj2") {
		t.Error("Expected view to contain project names in multi-project mode")
	}
}

// TestView_NoProjectTabs_SingleProject verifies tabs are NOT rendered in single-project mode.
func TestView_NoProjectTabs_SingleProject(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "single-proj",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	view := m.View()

	// Should NOT contain tab indicators like "All ("
	// (Project name itself might appear in status bar)
	if strings.Contains(view, "[All (") {
		t.Error("Expected view NOT to contain tab brackets '[All (' in single-project mode")
	}
}

// TestStatusBar_ShowsActiveProject verifies StatusBar.Project shows active project name.
func TestStatusBar_ShowsActiveProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Set active project to proj1
	m.projectTabs.SetActiveProject("proj1")
	m.activeProjectID = m.projectTabs.ActiveProject()
	m.syncActiveProjectView()

	// StatusBar should show "proj1"
	// Note: We check the StatusBar.Project field, not the rendered view
	// (rendered view may have styling that makes exact match difficult)
	if m.statusBar.Project != "proj1" {
		t.Errorf("Expected StatusBar.Project = 'proj1', got '%s'", m.statusBar.Project)
	}

	// Set active project to "all"
	m.projectTabs.SetActiveProject("all")
	m.activeProjectID = m.projectTabs.ActiveProject()
	m.syncActiveProjectView()

	// StatusBar should show "all"
	if m.statusBar.Project != "all" {
		t.Errorf("Expected StatusBar.Project = 'all', got '%s'", m.statusBar.Project)
	}
}
