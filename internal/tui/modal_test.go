package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ============================================================================
// Test Modal Implementation
// ============================================================================

// testModal is a simple modal implementation for testing.
type testModal struct {
	title       string
	content     string
	width       int
	height      int
	lastKey     string
	initCalled  bool
	keyHandled  bool
	updateCount int
}

func newTestModal(title, content string) *testModal {
	return &testModal{
		title:   title,
		content: content,
		width:   40,
		height:  10,
	}
}

func (m *testModal) Init() tea.Cmd {
	m.initCalled = true
	return nil
}

func (m *testModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	m.updateCount++
	return m, nil
}

func (m *testModal) View() string {
	return m.content
}

func (m *testModal) HandleKey(key string) (bool, tea.Cmd) {
	m.lastKey = key
	if key == "enter" {
		m.keyHandled = true
		return true, nil
	}
	return false, nil
}

func (m *testModal) Title() string {
	return m.title
}

func (m *testModal) Width() int {
	return m.width
}

func (m *testModal) Height() int {
	return m.height
}

// ============================================================================
// Modal Interface Tests
// ============================================================================

func TestModal_Interface(t *testing.T) {
	modal := newTestModal("Test", "Content")

	// Test Title
	if modal.Title() != "Test" {
		t.Errorf("Title() = %q, expected %q", modal.Title(), "Test")
	}

	// Test View
	if modal.View() != "Content" {
		t.Errorf("View() = %q, expected %q", modal.View(), "Content")
	}

	// Test Width/Height
	if modal.Width() != 40 {
		t.Errorf("Width() = %d, expected 40", modal.Width())
	}
	if modal.Height() != 10 {
		t.Errorf("Height() = %d, expected 10", modal.Height())
	}

	// Test Init
	cmd := modal.Init()
	if !modal.initCalled {
		t.Error("Init() did not set initCalled flag")
	}
	if cmd != nil {
		t.Error("Init() returned non-nil cmd")
	}

	// Test Update
	newModal, cmd := modal.Update(tea.KeyMsg{})
	if modal.updateCount != 1 {
		t.Errorf("Update() called %d times, expected 1", modal.updateCount)
	}
	if newModal == nil {
		t.Error("Update() returned nil modal")
	}
	if cmd != nil {
		t.Error("Update() returned non-nil cmd")
	}

	// Test HandleKey
	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Error("HandleKey('enter') should be handled")
	}
	if !modal.keyHandled {
		t.Error("HandleKey('enter') did not set keyHandled flag")
	}
	if modal.lastKey != "enter" {
		t.Errorf("HandleKey set lastKey = %q, expected 'enter'", modal.lastKey)
	}

	// Test unhandled key
	handled, _ = modal.HandleKey("unknown")
	if handled {
		t.Error("HandleKey('unknown') should not be handled")
	}
}

// ============================================================================
// ModalManager Tests
// ============================================================================

func TestModalManager_Creation(t *testing.T) {
	mgr := NewModalManager()

	if mgr.IsOpen() {
		t.Error("New ModalManager should not have an open modal")
	}

	if mgr.activeModal != nil {
		t.Error("activeModal should be nil after creation")
	}

	if len(mgr.stack) != 0 {
		t.Errorf("stack length = %d, expected 0", len(mgr.stack))
	}
}

func TestModalManager_OpenClose(t *testing.T) {
	mgr := NewModalManager()
	modal := newTestModal("Test Modal", "Test content")

	// Open modal
	cmd := mgr.Open(modal)
	if !mgr.IsOpen() {
		t.Error("IsOpen() should return true after Open()")
	}
	if mgr.activeModal == nil {
		t.Error("activeModal should not be nil after Open()")
	}
	if !modal.initCalled {
		t.Error("Modal Init() should be called on Open()")
	}
	// Note: testModal.Init() returns nil, so cmd should be nil
	// If modal returned a real command, Open() would pass it through

	// Close modal
	cmd = mgr.Close()
	if mgr.IsOpen() {
		t.Error("IsOpen() should return false after Close()")
	}
	if mgr.activeModal != nil {
		t.Error("activeModal should be nil after Close()")
	}
	if cmd != nil {
		t.Error("Close() should return nil cmd")
	}
}

func TestModalManager_Update(t *testing.T) {
	mgr := NewModalManager()
	modal := newTestModal("Test", "Content")
	mgr.Open(modal)

	// Send key message to manager
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	mgr, cmd := mgr.Update(keyMsg)

	if modal.updateCount != 1 {
		t.Errorf("Modal Update() called %d times, expected 1", modal.updateCount)
	}
	if cmd != nil {
		t.Error("Update() should return nil cmd when modal doesn't handle key")
	}
}

func TestModalManager_HandleKey_RoutesToModal(t *testing.T) {
	mgr := NewModalManager()
	modal := newTestModal("Test", "Content")
	mgr.Open(modal)

	// Send key that modal handles
	handled, cmd := mgr.HandleKey("enter")
	if !handled {
		t.Error("HandleKey should return true when modal handles key")
	}
	if modal.lastKey != "enter" {
		t.Errorf("Modal should receive key 'enter', got %q", modal.lastKey)
	}
	if cmd != nil {
		t.Error("HandleKey should return nil cmd")
	}
}

func TestModalManager_HandleKey_Unhandled(t *testing.T) {
	mgr := NewModalManager()
	modal := newTestModal("Test", "Content")
	mgr.Open(modal)

	// Send key that modal doesn't handle
	handled, cmd := mgr.HandleKey("unknown")
	if handled {
		t.Error("HandleKey should return false when modal doesn't handle key")
	}
	if cmd != nil {
		t.Error("HandleKey should return nil cmd for unhandled keys")
	}
}

func TestModalManager_HandleKey_EscCloses(t *testing.T) {
	mgr := NewModalManager()
	modal := newTestModal("Test", "Content")
	mgr.Open(modal)

	// Esc should close the modal
	handled, cmd := mgr.HandleKey("esc")
	if !handled {
		t.Error("HandleKey('esc') should be handled")
	}
	if mgr.IsOpen() {
		t.Error("Modal should be closed after Esc")
	}
	if cmd != nil {
		t.Error("HandleKey('esc') should return nil cmd")
	}
}

func TestModalManager_HandleKey_WhenClosed(t *testing.T) {
	mgr := NewModalManager()

	// HandleKey should do nothing when no modal is open
	handled, cmd := mgr.HandleKey("enter")
	if handled {
		t.Error("HandleKey should return false when no modal is open")
	}
	if cmd != nil {
		t.Error("HandleKey should return nil cmd when no modal is open")
	}
}

func TestModalManager_View_WhenClosed(t *testing.T) {
	mgr := NewModalManager()

	view := mgr.View(80, 24)
	if view != "" {
		t.Errorf("View() should return empty string when closed, got %q", view)
	}
}

func TestModalManager_View_Rendering(t *testing.T) {
	mgr := NewModalManager()
	modal := newTestModal("Test Modal", "Modal content here")
	mgr.Open(modal)

	view := mgr.View(80, 24)
	if view == "" {
		t.Error("View() should return non-empty string when modal is open")
	}

	// Check for modal content
	if !modalContains(view, "Modal content here") {
		t.Error("View() should contain modal content")
	}

	// Check for border (rounded border characters)
	if !modalContainsAny(view, "╭", "╮", "╯", "╰", "─", "│") {
		t.Error("View() should render border around modal")
	}
}

func TestModalManager_NestedModals(t *testing.T) {
	mgr := NewModalManager()
	modal1 := newTestModal("First", "First modal")
	modal2 := newTestModal("Second", "Second modal")

	// Open first modal
	mgr.Open(modal1)
	if !mgr.IsOpen() {
		t.Error("First modal should be open")
	}

	// Open second modal (should stack)
	mgr.Open(modal2)
	if !mgr.IsOpen() {
		t.Error("Second modal should be open")
	}
	if mgr.activeModal != modal2 {
		t.Error("Active modal should be the second modal")
	}
	if len(mgr.stack) != 1 {
		t.Errorf("Stack should contain 1 modal, got %d", len(mgr.stack))
	}

	// Close second modal
	mgr.Close()
	if !mgr.IsOpen() {
		t.Error("First modal should still be open")
	}
	if mgr.activeModal != modal1 {
		t.Error("Active modal should be the first modal after closing second")
	}

	// Close first modal
	mgr.Close()
	if mgr.IsOpen() {
		t.Error("No modal should be open")
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

func modalContains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && modalContainsRunes(s, substr)
}

func modalContainsRunes(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func modalContainsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if modalContains(s, substr) {
			return true
		}
	}
	return false
}
