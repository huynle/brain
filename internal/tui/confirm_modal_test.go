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
