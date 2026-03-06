package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// TestMetadataIntegration_OpenModalWithSKey tests that pressing 's' opens the metadata modal.
func TestMetadataIntegration_OpenModalWithSKey(t *testing.T) {
	// Create a test model with a selected task
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	m := NewModelWithContext(cfg, ctx)
	
	// Set up test tasks
	testTask := types.ResolvedTask{
		ID:       "test123",
		Title:    "Test Task",
		Status:   "active",
		Priority: "high",
	}
	
	m.tasks = []types.ResolvedTask{testTask}
	m.taskTree.SetTasks(m.tasks)
	
	// Move down to select the first task (navigate with j key)
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updatedM, _ := m.Update(jMsg)
	m = updatedM.(Model)
	
	
	// Verify modal is not open initially
	if m.modalManager.IsOpen() {
		t.Error("Expected modal to be closed initially")
	}
	
	// Press 's' key to open metadata modal
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedModel, cmd := m.Update(keyMsg)
	m = updatedModel.(Model)
	
	
	// Execute the Init command if returned
	if cmd != nil {
		_ = cmd() // This would normally fetch data
	}
	
	// Verify modal is now open
	if !m.modalManager.IsOpen() {
		t.Error("Expected modal to be open after pressing 's'")
	}
	
	// Verify it's a MetadataModal
	modal := m.modalManager.activeModal
	if _, ok := modal.(*MetadataModal); !ok {
		t.Errorf("Expected MetadataModal, got %T", modal)
	}
}

// TestMetadataIntegration_CloseModalWithEsc tests that pressing Esc closes the modal.
func TestMetadataIntegration_CloseModalWithEsc(t *testing.T) {
	// Create model with modal open
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	m := NewModelWithContext(cfg, ctx)
	
	// Create and open a metadata modal
	mockClient := &runner.APIClient{}
	modal := NewMetadataModal("test123", mockClient)
	m.modalManager.Open(modal)
	
	// Verify modal is open
	if !m.modalManager.IsOpen() {
		t.Fatal("Expected modal to be open before test")
	}
	
	// Press Esc to close
	escMsg := tea.KeyMsg{Type: tea.KeyEsc}
	updatedModel, _ := m.Update(escMsg)
	m = updatedModel.(Model)
	
	// Verify modal is closed
	if m.modalManager.IsOpen() {
		t.Error("Expected modal to be closed after pressing Esc")
	}
}

// TestMetadataIntegration_ModalHandlesKeysFirst tests that modal intercepts keys when open.
func TestMetadataIntegration_ModalHandlesKeysFirst(t *testing.T) {
	// Create model with modal open
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	m := NewModelWithContext(cfg, ctx)
	
	// Add test task
	testTask := types.ResolvedTask{
		ID:       "test123",
		Title:    "Test Task",
		Status:   "active",
		Priority: "high",
	}
	m.tasks = []types.ResolvedTask{testTask}
	m.taskTree.SetTasks(m.tasks)
	
	// Open metadata modal
	mockClient := &runner.APIClient{}
	modal := NewMetadataModal("test123", mockClient)
	m.modalManager.Open(modal)
	
	// Get initial focused index in modal
	initialIndex := modal.focusedIndex
	
	// Press 'j' - should be handled by modal (move down in fields)
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updatedModel, _ := m.Update(jMsg)
	m = updatedModel.(Model)
	
	// Verify modal handled the key (focused index changed)
	modal = m.modalManager.activeModal.(*MetadataModal)
	if modal.focusedIndex == initialIndex {
		t.Error("Expected modal to handle 'j' key and change focused index")
	}
}

// TestMetadataIntegration_ViewOverlaysModal tests that View() overlays modal when open.
func TestMetadataIntegration_ViewOverlaysModal(t *testing.T) {
	// Create model with size set
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	m := NewModelWithContext(cfg, ctx)
	m.width = 100
	m.height = 30
	
	// Get base view without modal
	baseView := m.View()
	
	// Open modal
	mockClient := &runner.APIClient{}
	modal := NewMetadataModal("test123", mockClient)
	m.modalManager.Open(modal)
	
	// Get view with modal
	viewWithModal := m.View()
	
	// View with modal should be different and contain modal title
	if baseView == viewWithModal {
		t.Error("Expected view to change when modal is open")
	}
	
	// Modal title should appear in view
	if !stringContains(viewWithModal, "Update Metadata") {
		t.Error("Expected view to contain modal title when modal is open")
	}
}

// TestMetadataIntegration_NoModalWhenNoTaskSelected tests that 's' does nothing without selection.
func TestMetadataIntegration_NoModalWhenNoTaskSelected(t *testing.T) {
	// Create model with no tasks
	cfg := Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	m := NewModelWithContext(cfg, ctx)
	m.tasks = []types.ResolvedTask{} // No tasks
	m.taskTree.SetTasks(m.tasks)
	
	// Press 's' key
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	updatedModel, _ := m.Update(keyMsg)
	m = updatedModel.(Model)
	
	// Modal should NOT open
	if m.modalManager.IsOpen() {
		t.Error("Expected modal to stay closed when no task is selected")
	}
}

// Helper function to check if string contains substring
func stringContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
