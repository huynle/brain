package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmModal_Interface(t *testing.T) {
	var _ Modal = (*ConfirmModal)(nil)
}

func TestNewConfirmModal(t *testing.T) {
	modal := NewConfirmModal("Delete task?", "This cannot be undone")

	if modal == nil {
		t.Fatal("NewConfirmModal returned nil")
	}

	if modal.title != "Delete task?" {
		t.Errorf("expected title 'Delete task?', got '%s'", modal.title)
	}

	if modal.message != "This cannot be undone" {
		t.Errorf("expected message 'This cannot be undone', got '%s'", modal.message)
	}

	if modal.confirmed {
		t.Error("expected confirmed to be false initially")
	}

	if modal.cancelled {
		t.Error("expected cancelled to be false initially")
	}
}

func TestConfirmModal_Title(t *testing.T) {
	modal := NewConfirmModal("Confirm Action", "Are you sure?")

	if got := modal.Title(); got != "Confirm Action" {
		t.Errorf("Title() = %q, want %q", got, "Confirm Action")
	}
}

func TestConfirmModal_View(t *testing.T) {
	modal := NewConfirmModal("Delete", "Are you sure?")

	view := modal.View()

	if view == "" {
		t.Error("View() returned empty string")
	}

	// View should contain message
	if !confirmedModalContains(view, "Are you sure?") {
		t.Error("View() should contain message")
	}

	// View should show y/n prompt
	if !confirmedModalContains(view, "[y/n]") {
		t.Error("View() should contain [y/n] prompt")
	}
}

func TestConfirmModal_HandleKey_Yes(t *testing.T) {
	modal := NewConfirmModal("Confirm", "Proceed?")

	handled, cmd := modal.HandleKey("y")

	if !handled {
		t.Error("HandleKey('y') should return handled=true")
	}

	if cmd == nil {
		t.Error("HandleKey('y') should return a command")
	}

	if !modal.confirmed {
		t.Error("pressing 'y' should set confirmed=true")
	}

	if modal.cancelled {
		t.Error("pressing 'y' should not set cancelled")
	}
}

func TestConfirmModal_HandleKey_CapitalYes(t *testing.T) {
	modal := NewConfirmModal("Confirm", "Proceed?")

	handled, cmd := modal.HandleKey("Y")

	if !handled {
		t.Error("HandleKey('Y') should return handled=true")
	}

	if cmd == nil {
		t.Error("HandleKey('Y') should return a command")
	}

	if !modal.confirmed {
		t.Error("pressing 'Y' should set confirmed=true")
	}
}

func TestConfirmModal_HandleKey_No(t *testing.T) {
	modal := NewConfirmModal("Confirm", "Proceed?")

	handled, cmd := modal.HandleKey("n")

	if !handled {
		t.Error("HandleKey('n') should return handled=true")
	}

	if cmd == nil {
		t.Error("HandleKey('n') should return a command")
	}

	if modal.confirmed {
		t.Error("pressing 'n' should not set confirmed")
	}

	if !modal.cancelled {
		t.Error("pressing 'n' should set cancelled=true")
	}
}

func TestConfirmModal_HandleKey_CapitalNo(t *testing.T) {
	modal := NewConfirmModal("Confirm", "Proceed?")

	handled, _ := modal.HandleKey("N")

	if !handled {
		t.Error("HandleKey('N') should return handled=true")
	}

	if !modal.cancelled {
		t.Error("pressing 'N' should set cancelled=true")
	}
}

func TestConfirmModal_HandleKey_Escape(t *testing.T) {
	modal := NewConfirmModal("Confirm", "Proceed?")

	// Escape should be handled by ModalManager, but modal can handle it too
	handled, _ := modal.HandleKey("esc")

	if !handled {
		t.Error("HandleKey('esc') should return handled=true")
	}

	if !modal.cancelled {
		t.Error("pressing Esc should set cancelled=true")
	}

	if modal.confirmed {
		t.Error("pressing Esc should not set confirmed")
	}
}

func TestConfirmModal_HandleKey_InvalidKey(t *testing.T) {
	modal := NewConfirmModal("Confirm", "Proceed?")

	handled, cmd := modal.HandleKey("x")

	if !handled {
		t.Error("Unknown keys should still be marked as handled to prevent passthrough")
	}

	if cmd != nil {
		t.Error("Unknown keys should return nil command")
	}

	if modal.confirmed {
		t.Error("pressing invalid key should not confirm")
	}

	if modal.cancelled {
		t.Error("pressing invalid key should not cancel")
	}
}

func TestConfirmModal_WithOnConfirm(t *testing.T) {
	_ = false // placeholder for called variable
	callback := func() tea.Msg {
		// called = true // callback will be called by HandleKey command execution
		return nil
	}

	modal := NewConfirmModal("Confirm", "Proceed?").
		WithOnConfirm(callback)

	// Press 'y' to confirm
	modal.HandleKey("y")

	// Callback should be stored but not called yet
	if modal.onConfirm == nil {
		t.Error("WithOnConfirm should set callback")
	}
}

func TestConfirmModal_WithOnCancel(t *testing.T) {
	callback := func() tea.Msg {
		return nil
	}

	modal := NewConfirmModal("Confirm", "Proceed?").
		WithOnCancel(callback)

	if modal.onCancel == nil {
		t.Error("WithOnCancel should set callback")
	}
}

func TestConfirmModal_Dimensions(t *testing.T) {
	modal := NewConfirmModal("Confirm", "A longer message that should affect dimensions")

	width := modal.Width()
	height := modal.Height()

	if width < 20 {
		t.Errorf("Width() = %d, want >= 20", width)
	}

	if height < 5 {
		t.Errorf("Height() = %d, want >= 5", height)
	}
}

func TestConfirmModal_Init(t *testing.T) {
	modal := NewConfirmModal("Confirm", "Proceed?")

	cmd := modal.Init()

	// Init should return nil (no initial command needed)
	if cmd != nil {
		t.Error("Init() should return nil for ConfirmModal")
	}
}

func TestConfirmModal_Update(t *testing.T) {
	modal := NewConfirmModal("Confirm", "Proceed?")

	// Update should just return the modal unchanged for most messages
	newModal, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	if newModal == nil {
		t.Error("Update() should return non-nil modal")
	}

	if cmd != nil {
		t.Error("Update() with unhandled key should return nil command")
	}
}

// --- Task title list tests ---

func TestConfirmModal_WithTaskTitles(t *testing.T) {
	titles := []string{"Fix login bug", "Add dark mode", "Update docs"}
	modal := NewConfirmModal("Delete Tasks", "Delete 3 task(s)?").
		WithTaskTitles(titles)

	if len(modal.taskTitles) != 3 {
		t.Errorf("expected 3 task titles, got %d", len(modal.taskTitles))
	}

	view := modal.View()

	// View should contain each title as a bullet
	for _, title := range titles {
		if !confirmedModalContains(view, title) {
			t.Errorf("View() should contain task title %q", title)
		}
	}

	// View should contain bullet markers
	if !confirmedModalContains(view, "•") {
		t.Error("View() should contain bullet markers")
	}
}

func TestConfirmModal_ViewTruncatesLongTitles(t *testing.T) {
	longTitle := "This is a very long task title that exceeds the maximum display length limit"
	modal := NewConfirmModal("Delete Task", "Delete 1 task(s)?").
		WithTaskTitles([]string{longTitle})

	view := modal.View()

	// Should NOT contain the full long title
	if confirmedModalContains(view, longTitle) {
		t.Error("View() should truncate long titles, but full title was found")
	}

	// Should contain the truncated prefix (first 39 chars)
	truncated := longTitle[:39]
	if !confirmedModalContains(view, truncated) {
		t.Errorf("View() should contain truncated title prefix %q", truncated)
	}
}

func TestConfirmModal_ViewShowsAndNMore(t *testing.T) {
	titles := make([]string, 8)
	for i := range titles {
		titles[i] = "Task " + string(rune('A'+i))
	}

	modal := NewConfirmModal("Delete Tasks", "Delete 8 task(s)?").
		WithTaskTitles(titles)

	view := modal.View()

	// Should show first 5 titles
	for i := 0; i < MaxVisibleTasks; i++ {
		if !confirmedModalContains(view, titles[i]) {
			t.Errorf("View() should contain visible title %q", titles[i])
		}
	}

	// Should NOT show titles beyond max
	for i := MaxVisibleTasks; i < len(titles); i++ {
		if confirmedModalContains(view, titles[i]) {
			t.Errorf("View() should NOT contain hidden title %q", titles[i])
		}
	}

	// Should show "and 3 more"
	if !confirmedModalContains(view, "and 3 more") {
		t.Error("View() should show 'and 3 more' for hidden titles")
	}
}

func TestConfirmModal_WithFeatureID(t *testing.T) {
	modal := NewConfirmModal("Delete Tasks", "Delete 3 task(s)?").
		WithFeatureID("auth-module")

	if modal.featureID != "auth-module" {
		t.Errorf("expected featureID 'auth-module', got %q", modal.featureID)
	}

	view := modal.View()

	if !confirmedModalContains(view, "feature: auth-module") {
		t.Error("View() should contain feature ID context")
	}
}

func TestConfirmModal_WithDestructive(t *testing.T) {
	modal := NewConfirmModal("Delete Task", "Delete 1 task(s)?").
		WithDestructive(true)

	if !modal.IsDestructive() {
		t.Error("IsDestructive() should return true after WithDestructive(true)")
	}

	// Default should be false
	modal2 := NewConfirmModal("Confirm", "Proceed?")
	if modal2.IsDestructive() {
		t.Error("IsDestructive() should return false by default")
	}
}

func TestConfirmModal_DestructiveModalInterface(t *testing.T) {
	modal := NewConfirmModal("Delete Task", "Delete?").
		WithDestructive(true)

	// Should satisfy DestructiveModal interface
	var _ DestructiveModal = modal
}

func TestConfirmModal_HeightIncreasesWithTaskTitles(t *testing.T) {
	modalNoTitles := NewConfirmModal("Confirm", "Proceed?")
	heightNoTitles := modalNoTitles.Height()

	modalWithTitles := NewConfirmModal("Delete Tasks", "Delete 3 task(s)?").
		WithTaskTitles([]string{"Task 1", "Task 2", "Task 3"})
	heightWithTitles := modalWithTitles.Height()

	if heightWithTitles <= heightNoTitles {
		t.Errorf("Height with task titles (%d) should be greater than without (%d)",
			heightWithTitles, heightNoTitles)
	}

	// With >5 titles, should include "and N more" line
	manyTitles := make([]string, 8)
	for i := range manyTitles {
		manyTitles[i] = "Task"
	}
	modalManyTitles := NewConfirmModal("Delete Tasks", "Delete 8 task(s)?").
		WithTaskTitles(manyTitles)
	heightManyTitles := modalManyTitles.Height()

	if heightManyTitles <= heightWithTitles {
		t.Errorf("Height with many titles (%d) should be greater than with 3 titles (%d)",
			heightManyTitles, heightWithTitles)
	}
}

// Helper function to check if string contains substring
func confirmedModalContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && confirmedModalContainsHelper(s, substr))
}

func confirmedModalContainsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
