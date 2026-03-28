package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

func TestStatusPickerModal_InitialState(t *testing.T) {
	// The current status should be pre-selected
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "active", apiClient)

	// Should have all 10 statuses
	if len(modal.statuses) != len(types.EntryStatuses) {
		t.Errorf("expected %d statuses, got %d", len(types.EntryStatuses), len(modal.statuses))
	}

	// "active" is at index 2 in EntryStatuses
	if modal.selectedIndex != 2 {
		t.Errorf("expected selectedIndex=2 for 'active', got %d", modal.selectedIndex)
	}

	if modal.currentStatus != "active" {
		t.Errorf("expected currentStatus='active', got %q", modal.currentStatus)
	}
}

func TestStatusPickerModal_InitialState_UnknownStatus(t *testing.T) {
	// Unknown status should default to index 0
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "nonexistent", apiClient)

	if modal.selectedIndex != 0 {
		t.Errorf("expected selectedIndex=0 for unknown status, got %d", modal.selectedIndex)
	}
}

func TestStatusPickerModal_NavigateDown(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	// Start at index 0 (draft)
	if modal.selectedIndex != 0 {
		t.Fatalf("expected initial index 0, got %d", modal.selectedIndex)
	}

	// Navigate down
	handled, _ := modal.HandleKey("j")
	if !handled {
		t.Error("expected key to be handled")
	}
	if modal.selectedIndex != 1 {
		t.Errorf("expected index 1 after j, got %d", modal.selectedIndex)
	}

	// Navigate down with arrow key
	handled, _ = modal.HandleKey("down")
	if !handled {
		t.Error("expected key to be handled")
	}
	if modal.selectedIndex != 2 {
		t.Errorf("expected index 2 after down, got %d", modal.selectedIndex)
	}
}

func TestStatusPickerModal_NavigateUp(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "active", apiClient)

	// Start at index 2 (active)
	if modal.selectedIndex != 2 {
		t.Fatalf("expected initial index 2, got %d", modal.selectedIndex)
	}

	// Navigate up
	handled, _ := modal.HandleKey("k")
	if !handled {
		t.Error("expected key to be handled")
	}
	if modal.selectedIndex != 1 {
		t.Errorf("expected index 1 after k, got %d", modal.selectedIndex)
	}
}

func TestStatusPickerModal_WrapDown(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "archived", apiClient)

	// "archived" is the last status (index 9)
	lastIndex := len(types.EntryStatuses) - 1
	if modal.selectedIndex != lastIndex {
		t.Fatalf("expected initial index %d, got %d", lastIndex, modal.selectedIndex)
	}

	// Navigate down should wrap to 0
	modal.HandleKey("j")
	if modal.selectedIndex != 0 {
		t.Errorf("expected wrap to 0, got %d", modal.selectedIndex)
	}
}

func TestStatusPickerModal_WrapUp(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	// Start at index 0 (draft)
	if modal.selectedIndex != 0 {
		t.Fatalf("expected initial index 0, got %d", modal.selectedIndex)
	}

	// Navigate up should wrap to last
	modal.HandleKey("k")
	lastIndex := len(types.EntryStatuses) - 1
	if modal.selectedIndex != lastIndex {
		t.Errorf("expected wrap to %d, got %d", lastIndex, modal.selectedIndex)
	}
}

func TestStatusPickerModal_EnterTriggersUpdate(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	// Navigate to "pending" (index 1)
	modal.HandleKey("j")

	// Press enter - should return a command (the API call)
	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Error("expected enter to be handled")
	}
	if cmd == nil {
		t.Error("expected a command from enter, got nil")
	}
}

func TestStatusPickerModal_EnterSameStatusSingleTask(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	// Press enter on current status (draft, index 0) - should return nil cmd (no-op)
	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Error("expected enter to be handled")
	}
	if cmd != nil {
		t.Error("expected nil command when selecting same status for single task")
	}
}

func TestStatusPickerModal_EscCancels(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	// Esc returns false so ModalManager can close the modal
	handled, cmd := modal.HandleKey("esc")
	if handled {
		t.Error("expected esc to return handled=false so ModalManager closes")
	}
	if cmd != nil {
		t.Error("expected nil command from esc")
	}
}

func TestStatusPickerModal_ViewRendersAllStatuses(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "active", apiClient)

	view := modal.View()

	// All 10 statuses should appear in the view
	for _, status := range types.EntryStatuses {
		if !strings.Contains(view, status) {
			t.Errorf("expected view to contain status %q", status)
		}
	}

	// Current status should show "(current)" marker
	if !strings.Contains(view, "(current)") {
		t.Error("expected view to contain '(current)' marker")
	}

	// Should show navigation help
	if !strings.Contains(view, "j/k") {
		t.Error("expected view to contain navigation help")
	}
}

func TestStatusPickerModal_ViewShowsArrowOnSelected(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	view := modal.View()

	// Should show arrow indicator on selected item
	if !strings.Contains(view, "→") {
		t.Error("expected view to contain '→' selection indicator")
	}
}

func TestStatusPickerModal_BatchMode(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	paths := []string{
		"projects/test/task/abc.md",
		"projects/test/task/def.md",
		"projects/test/task/ghi.md",
	}
	modal := NewStatusPickerModalBatch(paths, "pending", apiClient)

	if len(modal.taskPaths) != 3 {
		t.Errorf("expected 3 task paths, got %d", len(modal.taskPaths))
	}

	// View should show batch count
	view := modal.View()
	if !strings.Contains(view, "3 tasks selected") {
		t.Error("expected view to show '3 tasks selected'")
	}
}

func TestStatusPickerModal_Title(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	if modal.Title() != "Change Status" {
		t.Errorf("expected title 'Change Status', got %q", modal.Title())
	}
}

func TestStatusPickerModal_WidthHeight(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	if modal.Width() <= 0 {
		t.Error("expected positive width")
	}
	if modal.Height() <= 0 {
		t.Error("expected positive height")
	}

	// Batch mode should have greater height (batch header)
	batchModal := NewStatusPickerModalBatch([]string{"a", "b"}, "draft", apiClient)
	if batchModal.Height() <= modal.Height() {
		t.Error("expected batch modal to be taller than single modal")
	}
}

func TestStatusPickerModal_ConsumesUnknownKeys(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	// Unknown keys should be consumed (handled=true) to prevent passthrough
	handled, _ := modal.HandleKey("x")
	if !handled {
		t.Error("expected unknown key to be consumed")
	}
}

func TestStatusPickerModal_ImplementsModalInterface(t *testing.T) {
	apiClient := runner.NewAPIClient(runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"})
	modal := NewStatusPickerModal("projects/test/task/abc.md", "draft", apiClient)

	// Verify it satisfies the Modal interface
	var _ Modal = modal

	// Init should return nil
	cmd := modal.Init()
	if cmd != nil {
		t.Error("expected Init to return nil")
	}

	// Update should return self
	newModal, cmd := modal.Update(tea.KeyMsg{})
	if newModal != modal {
		t.Error("expected Update to return same modal")
	}
	if cmd != nil {
		t.Error("expected Update to return nil cmd")
	}
}
