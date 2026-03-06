package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestGroupTasksByFeature_Empty tests grouping with no tasks.
func TestGroupTasksByFeature_Empty(t *testing.T) {
	result := GroupTasksByFeature(nil)

	if len(result.Features) != 0 {
		t.Errorf("Expected 0 features, got %d", len(result.Features))
	}
	if result.Ungrouped != nil {
		t.Errorf("Expected nil ungrouped, got %+v", result.Ungrouped)
	}
}

// TestGroupTasksByFeature_AllUngrouped tests tasks without feature_id.
func TestGroupTasksByFeature_AllUngrouped(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", FeatureID: "", Priority: "high"},
		{ID: "task2", Title: "Task 2", FeatureID: "", Priority: "low"},
	}

	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 0 {
		t.Errorf("Expected 0 features, got %d", len(result.Features))
	}
	if result.Ungrouped == nil {
		t.Fatalf("Expected ungrouped group to exist")
	}
	if len(result.Ungrouped.Tasks) != 2 {
		t.Errorf("Expected 2 ungrouped tasks, got %d", len(result.Ungrouped.Tasks))
	}
	if result.Ungrouped.Name != "[Ungrouped]" {
		t.Errorf("Expected ungrouped name '[Ungrouped]', got %q", result.Ungrouped.Name)
	}
}

// TestGroupTasksByFeature_ByFeatureID tests grouping by feature_id.
func TestGroupTasksByFeature_ByFeatureID(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Auth task 1", FeatureID: "auth-system", Priority: "high"},
		{ID: "task2", Title: "Auth task 2", FeatureID: "auth-system", Priority: "low"},
		{ID: "task3", Title: "Dashboard task", FeatureID: "dashboard", Priority: "medium"},
		{ID: "task4", Title: "Ungrouped task", FeatureID: "", Priority: "medium"},
	}

	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 2 {
		t.Fatalf("Expected 2 features, got %d", len(result.Features))
	}

	// Check auth-system feature
	authFeature := findFeature(result.Features, "auth-system")
	if authFeature == nil {
		t.Fatalf("Expected auth-system feature to exist")
	}
	if len(authFeature.Tasks) != 2 {
		t.Errorf("Expected 2 tasks in auth-system, got %d", len(authFeature.Tasks))
	}

	// Check dashboard feature
	dashFeature := findFeature(result.Features, "dashboard")
	if dashFeature == nil {
		t.Fatalf("Expected dashboard feature to exist")
	}
	if len(dashFeature.Tasks) != 1 {
		t.Errorf("Expected 1 task in dashboard, got %d", len(dashFeature.Tasks))
	}

	// Check ungrouped
	if result.Ungrouped == nil {
		t.Fatalf("Expected ungrouped group to exist")
	}
	if len(result.Ungrouped.Tasks) != 1 {
		t.Errorf("Expected 1 ungrouped task, got %d", len(result.Ungrouped.Tasks))
	}
}

// TestGroupTasksByFeature_Sorting tests feature sorting by priority and name.
func TestGroupTasksByFeature_Sorting(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", FeatureID: "zzz-feature", FeaturePriority: "low"},
		{ID: "task2", FeatureID: "aaa-feature", FeaturePriority: "medium"},
		{ID: "task3", FeatureID: "bbb-feature", FeaturePriority: "high"},
		{ID: "task4", FeatureID: "ccc-feature", FeaturePriority: "high"},
	}

	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 4 {
		t.Fatalf("Expected 4 features, got %d", len(result.Features))
	}

	// Check ordering: high priority first (alphabetically), then medium, then low
	expected := []string{"bbb-feature", "ccc-feature", "aaa-feature", "zzz-feature"}
	for i, featureID := range expected {
		if result.Features[i].ID != featureID {
			t.Errorf("Expected feature[%d] to be %s, got %s", i, featureID, result.Features[i].ID)
		}
	}
}

// TestFeatureStats tests stat calculation for features.
func TestFeatureStats(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "completed", Classification: "ready"},
		{ID: "t2", Status: "in_progress", Classification: "ready"},
		{ID: "t3", Status: "pending", Classification: "waiting"},
		{ID: "t4", Status: "pending", Classification: "blocked"},
	}

	stats := computeFeatureStats(tasks)

	if stats.Total != 4 {
		t.Errorf("Expected total=4, got %d", stats.Total)
	}
	if stats.Completed != 1 {
		t.Errorf("Expected completed=1, got %d", stats.Completed)
	}
	if stats.Active != 1 {
		t.Errorf("Expected active=1, got %d", stats.Active)
	}
	if stats.Ready != 2 {
		t.Errorf("Expected ready=2, got %d", stats.Ready)
	}
	if stats.Waiting != 1 {
		t.Errorf("Expected waiting=1, got %d", stats.Waiting)
	}
	if stats.Blocked != 1 {
		t.Errorf("Expected blocked=1, got %d", stats.Blocked)
	}
}

// Helper function to find a feature by ID.
func findFeature(features []FeatureGroup, id string) *FeatureGroup {
	for i := range features {
		if features[i].ID == id {
			return &features[i]
		}
	}
	return nil
}
