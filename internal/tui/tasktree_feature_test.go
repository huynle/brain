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

// --- Tests for moveToTopFeatureGrouped / moveToBottomFeatureGrouped ---

// helper to build a feature-view TaskTree with given features and optional ungrouped.
func newFeatureViewTree(features []FeatureGroup, ungrouped *FeatureGroup) TaskTree {
	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.featureGroups = FeatureGroupResult{
		Features:  features,
		Ungrouped: ungrouped,
	}
	// Start in a neutral position
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.isOnUngrouped = false
	tt.SelectedID = ""
	return tt
}

func TestMoveToTopFeatureGrouped_JumpsToFirstFeatureHeader(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
		{ID: "feat-b", Tasks: []types.ResolvedTask{{ID: "t2"}}, Collapsed: false},
	}, nil)

	// Start somewhere in the middle
	tt.selectedFeatureIdx = 1
	tt.selectedFeatureTaskIdx = 0
	tt.SelectedID = "t2"

	tt.MoveToTop()

	if tt.selectedFeatureIdx != 0 {
		t.Errorf("Expected selectedFeatureIdx=0, got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected selectedFeatureTaskIdx=-1 (on header), got %d", tt.selectedFeatureTaskIdx)
	}
	if tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=false")
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID empty (on header), got %q", tt.SelectedID)
	}
}

func TestMoveToTopFeatureGrouped_NoFeatures_FallsToUngrouped(t *testing.T) {
	ungrouped := &FeatureGroup{
		ID:    "[Ungrouped]",
		Tasks: []types.ResolvedTask{{ID: "u1"}},
	}
	tt := newFeatureViewTree(nil, ungrouped)

	tt.MoveToTop()

	if !tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=true when no features exist")
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected on ungrouped header (-1), got %d", tt.selectedFeatureTaskIdx)
	}
}

func TestMoveToTopFeatureGrouped_EmptyTree(t *testing.T) {
	tt := newFeatureViewTree(nil, nil)

	// Should not panic
	tt.MoveToTop()

	if tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=false when no features and no ungrouped")
	}
}

func TestMoveToBottomFeatureGrouped_JumpsToLastTaskInUngrouped(t *testing.T) {
	ungrouped := &FeatureGroup{
		ID:        "[Ungrouped]",
		Tasks:     []types.ResolvedTask{{ID: "u1"}, {ID: "u2"}, {ID: "u3"}},
		Collapsed: false,
	}
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
	}, ungrouped)

	tt.MoveToBottom()

	if !tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=true (ungrouped is last section)")
	}
	if tt.SelectedID != "u3" {
		t.Errorf("Expected SelectedID=u3 (last ungrouped task), got %q", tt.SelectedID)
	}
	if tt.selectedFeatureTaskIdx != 2 {
		t.Errorf("Expected selectedFeatureTaskIdx=2, got %d", tt.selectedFeatureTaskIdx)
	}
}

func TestMoveToBottomFeatureGrouped_UngroupedCollapsed_LandsOnHeader(t *testing.T) {
	ungrouped := &FeatureGroup{
		ID:        "[Ungrouped]",
		Tasks:     []types.ResolvedTask{{ID: "u1"}},
		Collapsed: true,
	}
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
	}, ungrouped)

	tt.MoveToBottom()

	if !tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=true")
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected on ungrouped header (-1), got %d", tt.selectedFeatureTaskIdx)
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID empty (on header), got %q", tt.SelectedID)
	}
}

func TestMoveToBottomFeatureGrouped_NoUngrouped_JumpsToLastFeature(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
		{ID: "feat-b", Tasks: []types.ResolvedTask{{ID: "t2"}, {ID: "t3"}}, Collapsed: false},
	}, nil)

	tt.MoveToBottom()

	if tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=false (no ungrouped)")
	}
	if tt.selectedFeatureIdx != 1 {
		t.Errorf("Expected selectedFeatureIdx=1 (last feature), got %d", tt.selectedFeatureIdx)
	}
	if tt.SelectedID != "t3" {
		t.Errorf("Expected SelectedID=t3 (last task in last feature), got %q", tt.SelectedID)
	}
}

func TestMoveToBottomFeatureGrouped_LastFeatureCollapsed_LandsOnHeader(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
		{ID: "feat-b", Tasks: []types.ResolvedTask{{ID: "t2"}}, Collapsed: true},
	}, nil)

	tt.MoveToBottom()

	if tt.selectedFeatureIdx != 1 {
		t.Errorf("Expected selectedFeatureIdx=1 (last feature), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected on header (-1) when collapsed, got %d", tt.selectedFeatureTaskIdx)
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID empty (on header), got %q", tt.SelectedID)
	}
}

func TestMoveToBottomFeatureGrouped_EmptyTree(t *testing.T) {
	tt := newFeatureViewTree(nil, nil)

	// Should not panic
	tt.MoveToBottom()
}
