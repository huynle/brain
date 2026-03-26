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
		GroupVisible:      map[string]bool{"Draft": true, "Pending": true, "Active": true, "Blocked": true, "Inactive": false, "In Progress": false}, // Inactive and In Progress are hidden
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
	expectedGroups := []string{"Draft", "Pending", "Active", "Blocked", "Inactive"}
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
	// Draft should show ☑ with collapse indicator (visible=true)
	if !containsString(view, "☑ ▾ Draft") {
		t.Errorf("Expected 'Draft' to show checked box with collapse indicator (☑ ▾) when GroupVisible=true, got:\n%s", view)
	}
	// Inactive should show ☐ (visible=false, no collapse indicator)
	if !containsString(view, "☐") || !containsString(view, "Inactive") {
		t.Error("Expected 'Inactive' to show unchecked box (☐) when GroupVisible=false")
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

// TestSettingsModal_GroupsTabTaskCounts tests that task counts are displayed per group
func TestSettingsModal_GroupsTabTaskCounts(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		GroupVisible:      map[string]bool{"Draft": true, "Pending": true, "Inactive": true},
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	taskCounts := map[string]int{
		"Draft":    3,
		"Pending":  52,
		"Inactive": 10,
	}

	modal := NewSettingsModal(settings, taskCounts)
	modal.currentTab = TabGroups

	view := modal.View()

	// Should show task counts
	if !containsString(view, "(3)") {
		t.Errorf("Expected view to contain Draft task count '(3)', got:\n%s", view)
	}
	if !containsString(view, "(52)") {
		t.Errorf("Expected view to contain Pending task count '(52)', got:\n%s", view)
	}
	if !containsString(view, "(10)") {
		t.Errorf("Expected view to contain Completed task count '(10)', got:\n%s", view)
	}
}

// TestSettingsModal_GroupsTabCollapseIndicators tests collapse state indicators (▸/▾)
func TestSettingsModal_GroupsTabCollapseIndicators(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    map[string]bool{"Draft": true, "Pending": false},
		GroupVisible:      map[string]bool{"Draft": true, "Pending": true, "Inactive": false},
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	modal.currentTab = TabGroups

	view := modal.View()

	// Draft is visible and collapsed -> should show ▸
	if !containsString(view, "▸") {
		t.Errorf("Expected collapsed indicator ▸ for Draft (collapsed=true), got:\n%s", view)
	}
	// Pending is visible and expanded -> should show ▾
	if !containsString(view, "▾") {
		t.Errorf("Expected expanded indicator ▾ for Pending (collapsed=false), got:\n%s", view)
	}
	// Completed is hidden -> should NOT show collapse indicator for it
	// (space instead of ▸/▾)
}

// TestSettingsModal_GroupsTabCollapseToggle tests 'c' key toggles collapse state
func TestSettingsModal_GroupsTabCollapseToggle(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    map[string]bool{"Draft": false}, // Draft starts expanded
		GroupVisible:      map[string]bool{"Draft": true},
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	modal.currentTab = TabGroups
	modal.selectedIndex = 0 // Select Draft

	// Initially Draft should be expanded (collapsed=false)
	if modal.settings.GroupCollapsed["Draft"] {
		t.Error("Expected 'Draft' group to be expanded initially (collapsed=false)")
	}

	// Press 'c' to collapse the group
	handled, _ := modal.HandleKey("c")
	if !handled {
		t.Error("Expected 'c' key to be handled in Groups tab")
	}

	// Now Draft should be collapsed (collapsed=true)
	if !modal.settings.GroupCollapsed["Draft"] {
		t.Error("Expected 'Draft' group to be collapsed after 'c' toggle (collapsed=true)")
	}

	// Press 'c' again to expand it
	modal.HandleKey("c")
	if modal.settings.GroupCollapsed["Draft"] {
		t.Error("Expected 'Draft' group to be expanded after second 'c' toggle (collapsed=false)")
	}
}

// TestSettingsModal_GroupsTabCollapseOnlyWhenVisible tests that 'c' does nothing for hidden groups
func TestSettingsModal_GroupsTabCollapseOnlyWhenVisible(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    map[string]bool{},
		GroupVisible:      map[string]bool{"Draft": false}, // Draft is hidden
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	modal.currentTab = TabGroups
	modal.selectedIndex = 0 // Select Draft (hidden)

	// Press 'c' - should do nothing since group is hidden
	modal.HandleKey("c")

	// GroupCollapsed should not have been set for Draft
	if modal.settings.GroupCollapsed["Draft"] {
		t.Error("Expected 'c' to have no effect on hidden group")
	}
}

// TestSettingsModal_GroupsTabHelpText tests that help text includes collapse shortcut
func TestSettingsModal_GroupsTabHelpText(t *testing.T) {
	settings := Settings{
		GroupCollapsed:    make(map[string]bool),
		GroupVisible:      make(map[string]bool),
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	modal := NewSettingsModal(settings)
	modal.currentTab = TabGroups

	view := modal.View()

	if !containsString(view, "c: collapse") {
		t.Errorf("Expected help text to contain 'c: collapse', got:\n%s", view)
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
