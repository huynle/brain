package tui

import (
	"testing"
)

// TestSettingsModal_TabSwitching tests switching between Limits and Groups tabs
func TestSettingsModal_TabSwitching(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		GroupVisible:      make(map[string]bool),
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

	// Press Tab again to go to Runtime
	modal.HandleKey("tab")
	if modal.currentTab != TabRuntime {
		t.Errorf("Expected tab TabRuntime (2) after second tab press, got %d", modal.currentTab)
	}

	// Press Tab again to go to Monitors
	modal.HandleKey("tab")
	if modal.currentTab != TabMonitors {
		t.Errorf("Expected tab TabMonitors (3) after third tab press, got %d", modal.currentTab)
	}

	// Press Tab once more to cycle back to Limits
	modal.HandleKey("tab")
	if modal.currentTab != TabLimits {
		t.Errorf("Expected tab TabLimits (0) after fourth tab press, got %d", modal.currentTab)
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
		GroupCollapsed:    make(map[string]bool),
		GroupVisible:      map[string]bool{"Draft": true, "Pending": true, "Active": true, "Blocked": true, "Completed": false, "In Progress": false}, // Completed and In Progress are hidden
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
	expectedGroups := []string{"Draft", "Pending", "Active", "Blocked", "Completed"}
	for _, group := range expectedGroups {
		if !containsString(view, group) {
			t.Errorf("Expected view to contain group '%s'", group)
		}
	}

	// Should show checkboxes
	if !containsString(view, "☑") || !containsString(view, "☐") {
		t.Error("Expected view to contain checkbox symbols (☑ or ☐)")
	}

	// Verify specific checkbox states based on GroupVisible (not GroupCollapsed!)
	// Draft should show ☑ (visible=true)
	if !containsString(view, "☑ Draft") {
		t.Error("Expected 'Draft' to show checked box (☑) when GroupVisible=true")
	}
	// Completed should show ☐ (visible=false)
	if !containsString(view, "☐ Completed") {
		t.Error("Expected 'Completed' to show unchecked box (☐) when GroupVisible=false")
	}
}

// TestSettingsModal_GroupVisibilityToggle tests toggling group visibility with Space key
func TestSettingsModal_GroupVisibilityToggle(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		GroupVisible:      map[string]bool{"Draft": true}, // Draft starts visible
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	modal.currentTab = TabGroups
	modal.selectedIndex = 0 // Select first group (Draft)

	// Initially, Draft should be visible (GroupVisible["Draft"] = true)
	if !modal.settings.GroupVisible["Draft"] {
		t.Error("Expected 'Draft' group to be visible initially (GroupVisible=true)")
	}

	// Press Space to hide the group
	handled, _ := modal.HandleKey(" ")
	if !handled {
		t.Error("Expected space key to be handled in Groups tab")
	}

	// Now Draft should be hidden (GroupVisible["Draft"] = false)
	if modal.settings.GroupVisible["Draft"] {
		t.Error("Expected 'Draft' group to be hidden after space toggle (GroupVisible=false)")
	}

	// Press Space again to show it
	modal.HandleKey(" ")
	if !modal.settings.GroupVisible["Draft"] {
		t.Error("Expected 'Draft' group to be visible after second space toggle (GroupVisible=true)")
	}
}

// Helper function to check if a string contains a substring
func containsSubstring(s, substr string) bool {
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
