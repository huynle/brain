package tui

import (
	"fmt"
	"strings"
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

type mouseTestModal struct {
	*testModal
	clicked bool
	lastX   int
	lastY   int
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

func (m *mouseTestModal) HandleMouse(_ tea.MouseMsg, x, y int) (bool, tea.Cmd) {
	m.clicked = true
	m.lastX = x
	m.lastY = y
	return y == 0, nil
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
	handled, _ := modal.HandleKey("enter")
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
	// testModal.Init() returns nil, and Open() passes the modal's Init cmd
	// straight through.
	if cmd != nil {
		t.Error("Open() should pass through the modal's nil Init() cmd")
	}

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

func TestModalManager_HandleMouse_ComputesLayoutBeforeViewPersists(t *testing.T) {
	mgr := NewModalManager()
	modal := &mouseTestModal{testModal: newTestModal("Mouse", "one\ntwo\nthree")}
	mgr.Open(modal)

	// Model.View has a value receiver, so rendered content measurements are not
	// guaranteed to persist back to ModalManager before mouse hit testing.
	handled, cmd := mgr.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 31, Y: 14}, 100, 30)

	if !handled {
		t.Fatal("expected click on modal content line to be handled")
	}
	if cmd != nil {
		t.Fatalf("expected no command, got %v", cmd)
	}
	if !modal.clicked {
		t.Fatal("expected mouse modal to receive click")
	}
	if modal.lastX != 1 || modal.lastY != 0 {
		t.Fatalf("expected relative click (1,0), got (%d,%d)", modal.lastX, modal.lastY)
	}
}

func TestModalManager_HandleMouseWheelRoutesToJK(t *testing.T) {
	mgr := NewModalManager()
	modal := newFocusableTestModal(5)
	mgr.Open(modal)

	handled, cmd := mgr.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 1, Y: 1}, 100, 30)
	if !handled {
		t.Fatal("expected mouse wheel down to be handled by modal j navigation")
	}
	if cmd != nil {
		t.Fatalf("expected no command, got %v", cmd)
	}
	if modal.focusedIdx != 1 {
		t.Fatalf("expected wheel down to move focus like j to 1, got %d", modal.focusedIdx)
	}

	handled, cmd = mgr.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 1, Y: 1}, 100, 30)
	if !handled {
		t.Fatal("expected mouse wheel up to be handled by modal k navigation")
	}
	if cmd != nil {
		t.Fatalf("expected no command, got %v", cmd)
	}
	if modal.focusedIdx != 0 {
		t.Fatalf("expected wheel up to move focus like k to 0, got %d", modal.focusedIdx)
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
// Modal Scroll/Clamp Tests
// ============================================================================

func TestModalManager_View_ClampsToTerminalHeight(t *testing.T) {
	mgr := NewModalManager()

	// Create a modal with very tall content (30 lines)
	var lines string
	for i := 0; i < 30; i++ {
		lines += fmt.Sprintf("Line %d\n", i)
	}
	modal := newTestModal("Tall Modal", lines)
	mgr.Open(modal)

	// Render in a small terminal (20 rows)
	view := mgr.View(80, 20)

	// Count output lines - should not exceed terminal height
	outputLines := strings.Split(view, "\n")
	if len(outputLines) > 20 {
		t.Errorf("View() produced %d lines, should not exceed terminal height of 20", len(outputLines))
	}
}

func TestModalManager_View_ShowsScrollIndicator(t *testing.T) {
	mgr := NewModalManager()

	// Create a modal with tall content that will overflow
	var lines string
	for i := 0; i < 30; i++ {
		lines += fmt.Sprintf("Line %d\n", i)
	}
	modal := newTestModal("Tall Modal", lines)
	mgr.Open(modal)

	// Render in a small terminal
	view := mgr.View(80, 20)

	// Should show a scroll indicator (▼ or similar)
	if !strings.Contains(view, "▼") && !strings.Contains(view, "more") {
		t.Error("View() should show a scroll-down indicator when content overflows")
	}
}

func TestModalManager_ScrollDown(t *testing.T) {
	mgr := NewModalManager()

	// Create a modal with tall content
	var lines string
	for i := 0; i < 30; i++ {
		lines += fmt.Sprintf("Line %d\n", i)
	}
	modal := newTestModal("Tall Modal", lines)
	mgr.Open(modal)

	// Initial scroll offset should be 0
	if mgr.scrollOffset != 0 {
		t.Errorf("initial scrollOffset = %d, want 0", mgr.scrollOffset)
	}

	// Scroll down - should be handled by modal manager when content overflows
	// First render to establish content height
	mgr.View(80, 20)

	mgr.ScrollDown()
	if mgr.scrollOffset <= 0 {
		t.Error("scrollOffset should increase after ScrollDown()")
	}
}

func TestModalManager_ScrollUp(t *testing.T) {
	mgr := NewModalManager()

	var lines string
	for i := 0; i < 30; i++ {
		lines += fmt.Sprintf("Line %d\n", i)
	}
	modal := newTestModal("Tall Modal", lines)
	mgr.Open(modal)

	// Render to establish content height
	mgr.View(80, 20)

	// Scroll down first
	mgr.ScrollDown()
	mgr.ScrollDown()
	offset := mgr.scrollOffset

	// Scroll up
	mgr.ScrollUp()
	if mgr.scrollOffset >= offset {
		t.Error("scrollOffset should decrease after ScrollUp()")
	}
}

func TestModalManager_ScrollUp_ClampsToZero(t *testing.T) {
	mgr := NewModalManager()

	var lines string
	for i := 0; i < 30; i++ {
		lines += fmt.Sprintf("Line %d\n", i)
	}
	modal := newTestModal("Tall Modal", lines)
	mgr.Open(modal)

	mgr.View(80, 20)

	// Scroll up from 0 should stay at 0
	mgr.ScrollUp()
	if mgr.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0 (should not go negative)", mgr.scrollOffset)
	}
}

func TestModalManager_HandleKeyScrollsHelpModalWithJK(t *testing.T) {
	mgr := NewModalManager()
	mgr.Open(NewHelpModal(false))

	// Render first so the manager knows the modal content and viewport sizes.
	mgr.View(80, 20)
	if !mgr.NeedsScroll() {
		t.Fatal("expected help modal to need scrolling in a short viewport")
	}

	handled, cmd := mgr.HandleKey("j")
	if !handled {
		t.Fatal("expected j to be handled by help modal scrolling")
	}
	if cmd != nil {
		t.Fatalf("expected no command, got %v", cmd)
	}
	if mgr.scrollOffset == 0 {
		t.Fatal("expected j to scroll help modal down")
	}

	handled, cmd = mgr.HandleKey("k")
	if !handled {
		t.Fatal("expected k to be handled by help modal scrolling")
	}
	if cmd != nil {
		t.Fatalf("expected no command, got %v", cmd)
	}
	if mgr.scrollOffset != 0 {
		t.Fatalf("expected k to scroll help modal back up to 0, got %d", mgr.scrollOffset)
	}
}

func TestModalManager_Open_ResetsScroll(t *testing.T) {
	mgr := NewModalManager()

	var lines string
	for i := 0; i < 30; i++ {
		lines += fmt.Sprintf("Line %d\n", i)
	}
	modal1 := newTestModal("First", lines)
	mgr.Open(modal1)
	mgr.View(80, 20)
	mgr.ScrollDown()
	mgr.ScrollDown()

	if mgr.scrollOffset == 0 {
		t.Fatal("scrollOffset should be non-zero after scrolling")
	}

	// Open a new modal - scroll should reset
	modal2 := newTestModal("Second", "short content")
	mgr.Open(modal2)

	if mgr.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0 after opening new modal", mgr.scrollOffset)
	}
}

func TestModalManager_View_NoScrollForShortContent(t *testing.T) {
	mgr := NewModalManager()

	// Short content that fits in terminal
	modal := newTestModal("Short", "Just one line")
	mgr.Open(modal)

	view := mgr.View(80, 40)

	// Should NOT show scroll indicators
	if strings.Contains(view, "▼") || strings.Contains(view, "▲") {
		t.Error("View() should not show scroll indicators when content fits")
	}
}

func TestModalManager_View_WidthClamped(t *testing.T) {
	mgr := NewModalManager()

	// Create a modal with wide content
	modal := newTestModal("Wide", strings.Repeat("X", 200))
	mgr.Open(modal)

	// Render in narrow terminal
	view := mgr.View(40, 20)

	// Each line should not exceed terminal width
	for i, line := range strings.Split(view, "\n") {
		// Count visible characters (strip ANSI)
		if len(line) > 200 { // generous limit accounting for ANSI codes
			t.Errorf("line %d has %d chars, may exceed terminal width", i, len(line))
			break
		}
	}
}

// ============================================================================
// Auto-Scroll to Focused Item Tests
// ============================================================================

// focusableTestModal simulates a modal with navigable items using → indicator
type focusableTestModal struct {
	items      []string
	focusedIdx int
	totalItems int
}

func newFocusableTestModal(n int) *focusableTestModal {
	items := make([]string, n)
	for i := 0; i < n; i++ {
		items[i] = fmt.Sprintf("Item %d", i)
	}
	return &focusableTestModal{items: items, focusedIdx: 0, totalItems: n}
}

func (m *focusableTestModal) Init() tea.Cmd                       { return nil }
func (m *focusableTestModal) Update(msg tea.Msg) (Modal, tea.Cmd) { return m, nil }
func (m *focusableTestModal) Title() string                       { return "Focusable" }
func (m *focusableTestModal) Width() int                          { return 40 }
func (m *focusableTestModal) Height() int                         { return m.totalItems + 2 }

func (m *focusableTestModal) View() string {
	var b strings.Builder
	for i, item := range m.items {
		if i == m.focusedIdx {
			b.WriteString(fmt.Sprintf("→ %s\n", item))
		} else {
			b.WriteString(fmt.Sprintf("  %s\n", item))
		}
	}
	return b.String()
}

func (m *focusableTestModal) HandleKey(key string) (bool, tea.Cmd) {
	switch key {
	case "j":
		if m.focusedIdx < m.totalItems-1 {
			m.focusedIdx++
		}
		return true, nil
	case "k":
		if m.focusedIdx > 0 {
			m.focusedIdx--
		}
		return true, nil
	}
	return false, nil
}

func TestModalManager_AutoScrollFollowsFocus(t *testing.T) {
	mgr := NewModalManager()

	// 30 items, terminal height 15 → only ~9 items visible (15 - border/padding/title/indicator)
	modal := newFocusableTestModal(30)
	mgr.Open(modal)

	// Initial render - focus on item 0, scroll at 0
	mgr.View(80, 15)
	if mgr.scrollOffset != 0 {
		t.Fatalf("initial scrollOffset = %d, want 0", mgr.scrollOffset)
	}

	// Navigate down past the visible viewport
	for i := 0; i < 12; i++ {
		mgr.HandleKey("j")
		mgr.View(80, 15) // re-render to trigger auto-scroll
	}

	// Focus is now on item 12 - scroll should have adjusted to keep it visible
	if mgr.scrollOffset == 0 {
		t.Error("scrollOffset should have auto-scrolled to follow focus to item 12")
	}

	// The focused item (→ Item 12) should be in the rendered output
	view := mgr.View(80, 15)
	if !strings.Contains(view, "Item 12") {
		t.Error("focused item 'Item 12' should be visible in the viewport after auto-scroll")
	}
}

func TestModalManager_AutoScrollFollowsFocusUp(t *testing.T) {
	mgr := NewModalManager()

	modal := newFocusableTestModal(30)
	modal.focusedIdx = 20 // start focused near bottom
	mgr.Open(modal)

	// Render to establish scroll
	mgr.View(80, 15)

	// Navigate up past the visible viewport top
	for i := 0; i < 15; i++ {
		mgr.HandleKey("k")
		mgr.View(80, 15)
	}

	// Focus is now on item 5 - should be visible
	view := mgr.View(80, 15)
	if !strings.Contains(view, "Item 5") {
		t.Error("focused item 'Item 5' should be visible after scrolling up")
	}
}

func TestModalManager_TitleAlwaysVisible(t *testing.T) {
	mgr := NewModalManager()

	// Create a modal with tall content that overflows
	var lines string
	for i := 0; i < 30; i++ {
		lines += fmt.Sprintf("Line %d\n", i)
	}
	modal := newTestModal("My Title", lines)
	mgr.Open(modal)

	// Render in small terminal
	view := mgr.View(80, 15)

	// Title should always be visible regardless of scroll
	if !strings.Contains(view, "My Title") {
		t.Error("title should always be visible even when content overflows")
	}

	// Border should be visible (top border character)
	if !strings.Contains(view, "╭") {
		t.Error("top border should be visible")
	}
	if !strings.Contains(view, "╰") {
		t.Error("bottom border should be visible")
	}
}

func TestModalManager_TitleVisibleAfterScroll(t *testing.T) {
	mgr := NewModalManager()

	modal := newFocusableTestModal(30)
	mgr.Open(modal)

	// Navigate down to trigger scroll
	for i := 0; i < 15; i++ {
		mgr.HandleKey("j")
		mgr.View(80, 15)
	}

	// Title should still be visible after scrolling
	view := mgr.View(80, 15)
	if !strings.Contains(view, "Focusable") {
		t.Error("title should remain visible after scrolling down")
	}
}

func TestModalManager_NoAutoScrollWhenContentFits(t *testing.T) {
	mgr := NewModalManager()

	// Only 5 items - fits in any reasonable terminal
	modal := newFocusableTestModal(5)
	mgr.Open(modal)

	// Navigate to last item
	for i := 0; i < 4; i++ {
		mgr.HandleKey("j")
	}

	mgr.View(80, 40)

	// Should not need scrolling
	if mgr.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0 (content fits in terminal)", mgr.scrollOffset)
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
