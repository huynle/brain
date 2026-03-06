package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestGetSelectedFeatureID_NotInFeatureView tests that GetSelectedFeatureID
// returns empty string when not in feature view mode.
func TestGetSelectedFeatureID_NotInFeatureView(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = false
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = -1

	result := tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string when not in feature view, got %q", result)
	}
}

// TestGetSelectedFeatureID_OnTask tests that GetSelectedFeatureID
// returns empty string when cursor is on a task (not header).
func TestGetSelectedFeatureID_OnTask(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = 0 // On a task, not header

	// Set up a feature group
	tt.featureGroups = FeatureGroupResult{
		Features: []FeatureGroup{
			{
				ID:    "feat-123",
				Tasks: []types.ResolvedTask{{ID: "task-1"}},
			},
		},
	}

	result := tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string when on task, got %q", result)
	}
}

// TestGetSelectedFeatureID_OnUngrouped tests that GetSelectedFeatureID
// returns empty string when on ungrouped group header.
func TestGetSelectedFeatureID_OnUngrouped(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = true        // On ungrouped group

	result := tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string when on ungrouped, got %q", result)
	}
}

// TestGetSelectedFeatureID_OnFeatureHeader tests that GetSelectedFeatureID
// returns the feature ID when cursor is on a feature header.
func TestGetSelectedFeatureID_OnFeatureHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = false

	// Set up a feature group
	tt.featureGroups = FeatureGroupResult{
		Features: []FeatureGroup{
			{
				ID:    "feat-123",
				Tasks: []types.ResolvedTask{{ID: "task-1"}},
			},
		},
	}

	result := tt.GetSelectedFeatureID()
	expected := "feat-123"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestGetSelectedFeatureID_OnFeatureHeader_MultipleFeatures tests selection
// of different feature indices.
func TestGetSelectedFeatureID_OnFeatureHeader_MultipleFeatures(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = false

	// Set up multiple feature groups
	tt.featureGroups = FeatureGroupResult{
		Features: []FeatureGroup{
			{
				ID:    "feat-123",
				Tasks: []types.ResolvedTask{{ID: "task-1"}},
			},
			{
				ID:    "feat-456",
				Tasks: []types.ResolvedTask{{ID: "task-2"}},
			},
			{
				ID:    "feat-789",
				Tasks: []types.ResolvedTask{{ID: "task-3"}},
			},
		},
	}

	// Test selecting second feature
	tt.selectedFeatureIdx = 1
	result := tt.GetSelectedFeatureID()
	expected := "feat-456"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// Test selecting third feature
	tt.selectedFeatureIdx = 2
	result = tt.GetSelectedFeatureID()
	expected = "feat-789"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestGetSelectedFeatureID_OutOfBounds tests that GetSelectedFeatureID
// returns empty string when selectedFeatureIdx is out of bounds.
func TestGetSelectedFeatureID_OutOfBounds(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = false

	// Set up a feature group
	tt.featureGroups = FeatureGroupResult{
		Features: []FeatureGroup{
			{
				ID:    "feat-123",
				Tasks: []types.ResolvedTask{{ID: "task-1"}},
			},
		},
	}

	// Test negative index
	tt.selectedFeatureIdx = -1
	result := tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string for negative index, got %q", result)
	}

	// Test index too large
	tt.selectedFeatureIdx = 5
	result = tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string for out-of-bounds index, got %q", result)
	}
}
