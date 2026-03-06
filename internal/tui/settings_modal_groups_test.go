package tui

import (
	"testing"
)

// TestSettingsModal_TabSwitching tests switching between Limits and Groups tabs
func TestSettingsModal_TabSwitching(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     map[string]int{"project-a": 2},
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)

	// Initial tab should be Limits (TabLimits = 0)
	if modal.currentTab != TabLimits {
		t.Errorf("Expected initial tab TabLimits (0), got %d", modal.currentTab)
	}

	// Press Tab key to switch to Groups
	handled, _ := modal.HandleKey("tab")
	if !handled {
		t.Error("Expected 'tab' key to be handled")
	}
	if modal.currentTab != TabGroups {
		t.Errorf("Expected tab TabGroups (1) after tab press, got %d", modal.currentTab)
	}

	// Press Tab again to cycle back to Limits
	modal.HandleKey("tab")
	if modal.currentTab != TabLimits {
		t.Errorf("Expected tab TabLimits (0) after second tab press, got %d", modal.currentTab)
	}

	// Test direct navigation with '1' for Limits
	modal.currentTab = TabGroups
	handled, _ = modal.HandleKey("1")
	if !handled {
		t.Error("Expected '1' key to be handled")
	}
	if modal.currentTab != TabLimits {
		t.Errorf("Expected tab TabLimits (0) after pressing '1', got %d", modal.currentTab)
	}

	// Test direct navigation with '2' for Groups
	handled, _ = modal.HandleKey("2")
	if !handled {
		t.Error("Expected '2' key to be handled")
	}
	if modal.currentTab != TabGroups {
		t.Errorf("Expected tab TabGroups (1) after pressing '2', got %d", modal.currentTab)
	}
}

// TestSettingsModal_GroupsTabRendering tests that the Groups tab displays group visibility settings
func TestSettingsModal_GroupsTabRendering(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    map[string]bool{"Completed": true}, // Completed is hidden
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	modal.currentTab = TabGroups

	view := modal.View()

	// Check that Groups tab content is present
	if view == "" {
		t.Error("Expected non-empty view for Groups tab")
	}

	// Should contain group names
	expectedGroups := []string{"Ready", "Waiting", "Active", "Blocked", "Completed"}
	for _, group := range expectedGroups {
		if !containsString(view, group) {
			t.Errorf("Expected view to contain group '%s'", group)
		}
	}

	// Should show checkboxes
	if !containsString(view, "☑") || !containsString(view, "☐") {
		t.Error("Expected view to contain checkbox symbols (☑ or ☐)")
	}
}

// TestSettingsModal_GroupVisibilityToggle tests toggling group visibility with Space key
func TestSettingsModal_GroupVisibilityToggle(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	modal.currentTab = TabGroups
	modal.selectedIndex = 0 // Select first group (Ready)

	// Initially, all groups should be visible (not in GroupCollapsed map)
	if modal.settings.GroupCollapsed["Ready"] {
		t.Error("Expected 'Ready' group to be visible initially")
	}

	// Press Space to hide the group
	handled, _ := modal.HandleKey(" ")
	if !handled {
		t.Error("Expected space key to be handled in Groups tab")
	}

	// Now Ready should be hidden (GroupCollapsed["Ready"] = true)
	if !modal.settings.GroupCollapsed["Ready"] {
		t.Error("Expected 'Ready' group to be hidden after space toggle")
	}

	// Press Space again to show it
	modal.HandleKey(" ")
	if modal.settings.GroupCollapsed["Ready"] {
		t.Error("Expected 'Ready' group to be visible after second space toggle")
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && indexOfString(s, substr) >= 0
}

func indexOfString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
