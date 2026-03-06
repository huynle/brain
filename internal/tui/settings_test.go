package tui

import (
	"encoding/json"
	"testing"
)

// TestSettings_DefaultGroupVisibility tests that getDefaultGroupVisible returns correct defaults
func TestSettings_DefaultGroupVisibility(t *testing.T) {
	// Test the default function directly
	defaults := getDefaultGroupVisible()

	// Define expected default visibility
	expectedVisible := map[string]bool{
		"Ready":      true,
		"Waiting":    true,
		"Active":     true,
		"Blocked":    true,
		"Completed":  true,
		"Validated":  true,
		"Draft":      false,
		"Cancelled":  false,
		"Superseded": false,
		"Archived":   false,
	}

	// Verify all expected defaults are set correctly
	for group, expectedVisibility := range expectedVisible {
		if defaults[group] != expectedVisibility {
			t.Errorf("Expected getDefaultGroupVisible()[%s] = %v, got %v", group, expectedVisibility, defaults[group])
		}
	}
}

// TestSettings_LoadSettingsWithGroupVisible tests that LoadSettings properly initializes GroupVisible
func TestSettings_LoadSettingsWithGroupVisible(t *testing.T) {
	// Test loading settings (will use real ~/.brain path or create defaults)
	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}

	if settings.GroupVisible == nil {
		t.Fatal("Expected GroupVisible to be initialized, got nil")
	}

	// Verify default visibility for key groups
	// Note: This test assumes defaults or previously saved settings
	// The key is that GroupVisible should be initialized
	if len(settings.GroupVisible) == 0 {
		// If empty, it should have been populated with defaults
		t.Error("Expected GroupVisible to be populated with defaults")
	}
}

// TestSettings_JSONMarshalGroupVisible tests that GroupVisible is properly marshaled to JSON
func TestSettings_JSONMarshalGroupVisible(t *testing.T) {
	settings := Settings{
		GroupCollapsed: make(map[string]bool),
		GroupVisible: map[string]bool{
			"Ready":   true,
			"Draft":   false,
			"Blocked": true,
		},
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	// Marshal to JSON
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("Failed to marshal settings: %v", err)
	}

	// Unmarshal back
	var unmarshaled Settings
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal settings: %v", err)
	}

	// Verify GroupVisible is preserved
	if unmarshaled.GroupVisible["Ready"] != true {
		t.Error("Expected 'Ready' to be true after unmarshal")
	}
	if unmarshaled.GroupVisible["Draft"] != false {
		t.Error("Expected 'Draft' to be false after unmarshal")
	}
	if unmarshaled.GroupVisible["Blocked"] != true {
		t.Error("Expected 'Blocked' to be true after unmarshal")
	}
}

// TestSettings_SaveAndLoadGroupVisible tests round-trip persistence of GroupVisible
func TestSettings_SaveAndLoadGroupVisible(t *testing.T) {
	// Test JSON round-trip (this tests the JSON marshaling behavior)
	originalSettings := Settings{
		GroupCollapsed: make(map[string]bool),
		GroupVisible: map[string]bool{
			"Ready":     false, // Hide Ready
			"Draft":     true,  // Show Draft (opposite of default)
			"Completed": true,
		},
		FeatureCollapsed:  make(map[string]bool),
		ProjectLimits:     make(map[string]int),
		GlobalMaxParallel: 4,
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(originalSettings, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal settings: %v", err)
	}

	// Unmarshal back (simulating LoadSettings logic)
	var loadedSettings Settings
	if err := json.Unmarshal(data, &loadedSettings); err != nil {
		t.Fatalf("Failed to unmarshal settings: %v", err)
	}

	// Verify GroupVisible was persisted correctly
	if loadedSettings.GroupVisible["Ready"] != false {
		t.Error("Expected 'Ready' to be false after load")
	}
	if loadedSettings.GroupVisible["Draft"] != true {
		t.Error("Expected 'Draft' to be true after load")
	}
	if loadedSettings.GroupVisible["Completed"] != true {
		t.Error("Expected 'Completed' to be true after load")
	}
}
