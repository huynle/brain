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

// TestUpdate_HKey_PrevTab verifies 'H' (shift) navigates to previous project tab.
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

	// Press 'H' (shift) — project navigation
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should move to tab 0 (All)
	if model.projectTabs.ActiveIndex != 0 {
		t.Errorf("Expected tab index 0 after 'H', got %d", model.projectTabs.ActiveIndex)
	}
}

// TestUpdate_LKey_NextTab verifies 'L' (shift) navigates to next project tab.
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

	// Press 'L' (shift) — project navigation
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should move to tab 1 (proj1)
	if model.projectTabs.ActiveIndex != 1 {
		t.Errorf("Expected tab index 1 after 'L', got %d", model.projectTabs.ActiveIndex)
	}
}

// TestUpdate_LeftBracket_NoProjectChange verifies '[' switches content tabs, not
// projects (project index is unchanged by bracket keys under the new mapping).
func TestUpdate_LeftBracket_NoProjectChange(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.projectTabs.ActiveIndex = 1

	// Press '[' — content-tab navigation, must not move the project tab
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Project tab unchanged
	if model.projectTabs.ActiveIndex != 1 {
		t.Errorf("Expected project tab index unchanged (1) after '[', got %d", model.projectTabs.ActiveIndex)
	}
}

// TestUpdate_RightBracket_NoProjectChange verifies ']' switches content tabs,
// not projects (project index is unchanged by bracket keys).
func TestUpdate_RightBracket_NoProjectChange(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.projectTabs.ActiveIndex = 0

	// Press ']' — content-tab navigation, must not move the project tab
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Project tab unchanged
	if model.projectTabs.ActiveIndex != 0 {
		t.Errorf("Expected project tab index unchanged (0) after ']', got %d", model.projectTabs.ActiveIndex)
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

	// Press 'L' (shift) to switch to proj1
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Should show only proj1 tasks (1 task)
	if len(model.tasks) != 1 {
		t.Errorf("Expected 1 task for proj1, got %d", len(model.tasks))
	}

	// Press 'L' again to switch to proj2
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}}
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

// TestProjectTabs_ViewNotRendered verifies the project-tab row is intentionally
// not rendered — projects are switched with H/L and shown in the status bar.
func TestProjectTabs_ViewNotRendered(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40
	m.projectTabs.UpdateStats("proj1", TaskStats{Ready: 1})

	// The ProjectTabs.View() renders nothing regardless of project count.
	if got := m.projectTabs.View(m.width); got != "" {
		t.Errorf("expected ProjectTabs.View to render nothing, got %q", got)
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
