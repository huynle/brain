package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestNestedNavigation_MoveUpFromFeatureHeader_LandsOnPreviousFeatureTasks tests the fix for:
// "k (up) from feature header skips expanded tasks, jumps to previous header"
//
// Expected behavior: When pressing 'k' from a feature header, if the previous feature
// is expanded with tasks, the cursor should land on the LAST TASK of that feature,
// not on the feature header.
func TestNestedNavigation_MoveUpFromFeatureHeader_LandsOnPreviousFeatureTasks(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)         // grouped view
	tt.SetFeatureViewMode(false) // explicitly use nested status+feature view

	tasks := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task A1", "ready", "high", "pending", "feat-a"),
		makeTaskWithStatusAndFeature("t2", "Task A2", "ready", "medium", "pending", "feat-a"),
		makeTaskWithStatusAndFeature("t3", "Task A3", "ready", "low", "pending", "feat-a"),
		makeTaskWithStatusAndFeature("t4", "Task B1", "ready", "high", "pending", "feat-b"),
		makeTaskWithStatusAndFeature("t5", "Task B2", "ready", "medium", "pending", "feat-b"),
	}
	tt.tasks = tasks

	// Set up nested structure: one status group with two features
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{
					ID:        "feat-a",
					Name:      "Feature A",
					Tasks:     tasks[0:3], // Task A1, A2, A3
					Stats:     FeatureStats{Total: 3, Completed: 0},
					Collapsed: false, // EXPANDED
				},
				{
					ID:        "feat-b",
					Name:      "Feature B",
					Tasks:     tasks[3:5], // Task B1, B2
					Stats:     FeatureStats{Total: 2, Completed: 0},
					Collapsed: false, // EXPANDED
				},
			},
			Count:     5,
			Collapsed: false, // EXPANDED
		},
	}

	// Start on Feature B header (second feature)
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = 1 // Feature B
	tt.selectedTaskIdx = -1   // on header
	tt.isOnStatusHeader = false
	tt.SelectedID = ""

	// Move up - should land on Task A3 (last task of Feature A)
	tt.moveUpNestedGrouped()

	// Verify we're on Feature A's last task
	if tt.selectedStatusIdx != 0 {
		t.Errorf("Expected selectedStatusIdx=0, got %d", tt.selectedStatusIdx)
	}
	if tt.selectedFeatureIdx != 0 {
		t.Errorf("Expected selectedFeatureIdx=0 (Feature A), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedTaskIdx != 2 {
		t.Errorf("Expected selectedTaskIdx=2 (last task index in Feature A), got %d", tt.selectedTaskIdx)
	}
	if tt.SelectedID != "t3" {
		t.Errorf("Expected SelectedID='t3' (Task A3), got '%s'", tt.SelectedID)
	}
	if tt.isOnStatusHeader {
		t.Error("Expected isOnStatusHeader=false")
	}
}

// TestNestedNavigation_MoveUpFromFeatureHeader_CollapsedFeature tests that when
// the previous feature is COLLAPSED, we land on its header (not on tasks).
func TestNestedNavigation_MoveUpFromFeatureHeader_CollapsedFeature(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(false) // explicitly use nested status+feature view

	tasks := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task A1", "ready", "high", "pending", "feat-a"),
		makeTaskWithStatusAndFeature("t2", "Task B1", "ready", "high", "pending", "feat-b"),
	}
	tt.tasks = tasks

	// Set up nested structure: Feature A is COLLAPSED
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{
					ID:        "feat-a",
					Name:      "Feature A",
					Tasks:     tasks[0:1],
					Stats:     FeatureStats{Total: 1, Completed: 0},
					Collapsed: true, // COLLAPSED
				},
				{
					ID:        "feat-b",
					Name:      "Feature B",
					Tasks:     tasks[1:2],
					Stats:     FeatureStats{Total: 1, Completed: 0},
					Collapsed: false,
				},
			},
			Count:     2,
			Collapsed: false,
		},
	}

	// Start on Feature B header
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = 1
	tt.selectedTaskIdx = -1
	tt.isOnStatusHeader = false
	tt.SelectedID = ""

	// Move up - should land on Feature A header (not tasks, since collapsed)
	tt.moveUpNestedGrouped()

	// Verify we're on Feature A's header
	if tt.selectedStatusIdx != 0 {
		t.Errorf("Expected selectedStatusIdx=0, got %d", tt.selectedStatusIdx)
	}
	if tt.selectedFeatureIdx != 0 {
		t.Errorf("Expected selectedFeatureIdx=0 (Feature A), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedTaskIdx != -1 {
		t.Errorf("Expected selectedTaskIdx=-1 (on header), got %d", tt.selectedTaskIdx)
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID='' (on header), got '%s'", tt.SelectedID)
	}
	if tt.isOnStatusHeader {
		t.Error("Expected isOnStatusHeader=false")
	}
}
