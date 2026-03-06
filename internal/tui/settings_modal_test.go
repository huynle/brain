package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSettingsModal_Creation(t *testing.T) {
	// Create settings with some initial data
	settings := Settings{
		GroupCollapsed:   make(map[string]bool),
		FeatureCollapsed: make(map[string]bool),
		ProjectLimits: map[string]int{
			"project-a": 2,
			"project-b": 0, // unlimited
		},
		GlobalMaxParallel: 4,
	}

	// Create modal
	modal := NewSettingsModal(settings)

	// Verify modal implements Modal interface
	var _ Modal = modal

	// Verify title
	if modal.Title() != "Settings" {
		t.Errorf("Expected title 'Settings', got '%s'", modal.Title())
	}

	// Verify dimensions
	if modal.Width() <= 0 {
		t.Errorf("Expected positive width, got %d", modal.Width())
	}
	if modal.Height() <= 0 {
		t.Errorf("Expected positive height, got %d", modal.Height())
	}
}

func TestSettingsModal_Navigation(t *testing.T) {
	settings := Settings{
		GroupCollapsed:   make(map[string]bool),
		FeatureCollapsed: make(map[string]bool),
		ProjectLimits: map[string]int{
			"project-a": 2,
			"project-b": 3,
			"project-c": 0,
		},
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)

	// Initial selected index should be 0 (global max-parallel)
	if modal.selectedIndex != 0 {
		t.Errorf("Expected initial selectedIndex 0, got %d", modal.selectedIndex)
	}

	// Navigate down with 'j'
	handled, _ := modal.HandleKey("j")
	if !handled {
		t.Error("Expected 'j' key to be handled")
	}
	if modal.selectedIndex != 1 {
		t.Errorf("After 'j', expected selectedIndex 1, got %d", modal.selectedIndex)
	}

	// Navigate down again
	modal.HandleKey("j")
	if modal.selectedIndex != 2 {
		t.Errorf("After second 'j', expected selectedIndex 2, got %d", modal.selectedIndex)
	}

	// Navigate up with 'k'
	handled, _ = modal.HandleKey("k")
	if !handled {
		t.Error("Expected 'k' key to be handled")
	}
	if modal.selectedIndex != 1 {
		t.Errorf("After 'k', expected selectedIndex 1, got %d", modal.selectedIndex)
	}

	// Navigate to top
	modal.HandleKey("k")
	if modal.selectedIndex != 0 {
		t.Errorf("Expected selectedIndex 0, got %d", modal.selectedIndex)
	}

	// 'k' at top should stay at top
	modal.HandleKey("k")
	if modal.selectedIndex != 0 {
		t.Errorf("Expected selectedIndex to stay at 0, got %d", modal.selectedIndex)
	}
}

func TestSettingsModal_AdjustLimits(t *testing.T) {
	settings := Settings{
		GroupCollapsed:   make(map[string]bool),
		FeatureCollapsed: make(map[string]bool),
		ProjectLimits: map[string]int{
			"project-a": 2,
		},
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)

	// Increase global max-parallel with '+'
	handled, _ := modal.HandleKey("+")
	if !handled {
		t.Error("Expected '+' key to be handled")
	}
	if modal.settings.GlobalMaxParallel != 5 {
		t.Errorf("Expected GlobalMaxParallel 5, got %d", modal.settings.GlobalMaxParallel)
	}

	// Decrease with '-'
	handled, _ = modal.HandleKey("-")
	if !handled {
		t.Error("Expected '-' key to be handled")
	}
	if modal.settings.GlobalMaxParallel != 4 {
		t.Errorf("Expected GlobalMaxParallel 4, got %d", modal.settings.GlobalMaxParallel)
	}

	// Cannot decrease below 1
	modal.HandleKey("-")
	modal.HandleKey("-")
	modal.HandleKey("-")
	modal.HandleKey("-")
	if modal.settings.GlobalMaxParallel < 1 {
		t.Errorf("Expected GlobalMaxParallel >= 1, got %d", modal.settings.GlobalMaxParallel)
	}

	// Navigate to project and adjust
	modal.HandleKey("j") // Move to project-a
	initialLimit := modal.settings.ProjectLimits["project-a"]

	modal.HandleKey("+")
	if modal.settings.ProjectLimits["project-a"] != initialLimit+1 {
		t.Errorf("Expected project-a limit %d, got %d", initialLimit+1, modal.settings.ProjectLimits["project-a"])
	}

	modal.HandleKey("-")
	if modal.settings.ProjectLimits["project-a"] != initialLimit {
		t.Errorf("Expected project-a limit %d, got %d", initialLimit, modal.settings.ProjectLimits["project-a"])
	}
}

func TestSettingsModal_UnlimitedToggle(t *testing.T) {
	settings := Settings{
		GroupCollapsed:   make(map[string]bool),
		FeatureCollapsed: make(map[string]bool),
		ProjectLimits: map[string]int{
			"project-a": 2,
		},
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)

	// Navigate to project
	modal.HandleKey("j")

	// Press '0' to set unlimited
	handled, _ := modal.HandleKey("0")
	if !handled {
		t.Error("Expected '0' key to be handled")
	}
	if modal.settings.ProjectLimits["project-a"] != 0 {
		t.Errorf("Expected project-a limit 0 (unlimited), got %d", modal.settings.ProjectLimits["project-a"])
	}

	// Press '0' again should keep it at 0
	modal.HandleKey("0")
	if modal.settings.ProjectLimits["project-a"] != 0 {
		t.Errorf("Expected project-a limit to stay at 0, got %d", modal.settings.ProjectLimits["project-a"])
	}

	// Pressing '+' should set to 1 (from unlimited)
	modal.HandleKey("+")
	if modal.settings.ProjectLimits["project-a"] != 1 {
		t.Errorf("Expected project-a limit 1, got %d", modal.settings.ProjectLimits["project-a"])
	}
}

func TestSettingsModal_Init(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     map[string]int{},
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	cmd := modal.Init()

	// Init should return nil command for this modal
	if cmd != nil {
		t.Error("Expected Init to return nil command")
	}
}

func TestSettingsModal_Update(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     map[string]int{},
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)

	// Update with a message
	newModal, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	// Should return the same type
	if _, ok := newModal.(*SettingsModal); !ok {
		t.Error("Expected Update to return *SettingsModal")
	}

	// Command should be nil for this modal
	if cmd != nil {
		t.Error("Expected Update to return nil command")
	}
}

func TestSettingsModal_Update_RoutesKeyMsg(t *testing.T) {
	settings := Settings{
		GroupCollapsed:   make(map[string]bool),
		FeatureCollapsed: make(map[string]bool),
		ProjectLimits: map[string]int{
			"project-a": 2,
		},
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	initialIndex := modal.selectedIndex

	// Send 'j' key via Update() - should route to HandleKey() and move selection down
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	newModal, cmd := modal.Update(keyMsg)

	// Should return same modal type
	if _, ok := newModal.(*SettingsModal); !ok {
		t.Error("Expected Update to return *SettingsModal")
	}

	// Should route to HandleKey and update selectedIndex
	updatedModal := newModal.(*SettingsModal)
	if updatedModal.selectedIndex == initialIndex {
		t.Errorf("Expected selectedIndex to change from %d after 'j' key, but it stayed the same", initialIndex)
	}
	if updatedModal.selectedIndex != initialIndex+1 {
		t.Errorf("Expected selectedIndex to be %d after 'j' key, got %d", initialIndex+1, updatedModal.selectedIndex)
	}

	// Command should be nil
	if cmd != nil {
		t.Error("Expected Update to return nil command")
	}
}

func TestSettingsModal_Update_HandlesSaveMessage(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     map[string]int{},
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)

	// Simulate a successful save
	successMsg := settingsSavedMsg{err: nil}
	newModal, cmd := modal.Update(successMsg)

	updatedModal := newModal.(*SettingsModal)
	if !updatedModal.saveSuccess {
		t.Error("Expected saveSuccess to be true after successful save message")
	}
	if updatedModal.saveError != nil {
		t.Error("Expected saveError to be nil after successful save message")
	}
	if cmd != nil {
		t.Error("Expected Update to return nil command")
	}

	// Simulate a failed save
	modal2 := NewSettingsModal(settings)
	testErr := fmt.Errorf("save failed")
	failMsg := settingsSavedMsg{err: testErr}
	newModal2, cmd2 := modal2.Update(failMsg)

	updatedModal2 := newModal2.(*SettingsModal)
	if updatedModal2.saveSuccess {
		t.Error("Expected saveSuccess to be false after failed save message")
	}
	if updatedModal2.saveError == nil {
		t.Error("Expected saveError to be set after failed save message")
	}
	if updatedModal2.saveError != testErr {
		t.Errorf("Expected saveError to be %v, got %v", testErr, updatedModal2.saveError)
	}
	if cmd2 != nil {
		t.Error("Expected Update to return nil command")
	}
}
