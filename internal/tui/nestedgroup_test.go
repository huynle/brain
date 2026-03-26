package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestGroupTasksByStatusAndFeature_Empty tests grouping with no tasks.
func TestGroupTasksByStatusAndFeature_Empty(t *testing.T) {
	result := GroupTasksByStatusAndFeature(nil, nil)
	if len(result) != 0 {
		t.Errorf("Expected 0 groups, got %d", len(result))
	}

	result = GroupTasksByStatusAndFeature([]types.ResolvedTask{}, nil)
	if len(result) != 0 {
		t.Errorf("Expected 0 groups for empty slice, got %d", len(result))
	}
}

// TestGroupTasksByStatusAndFeature_SingleStatusNoFeatures tests grouping with single status and no feature_id.
func TestGroupTasksByStatusAndFeature_SingleStatusNoFeatures(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", Classification: "ready", Status: "pending"},
		{ID: "task2", Title: "Task 2", Classification: "ready", Status: "pending"},
	}

	result := GroupTasksByStatusAndFeature(tasks, nil)

	// With the new behavior, tasks without feature_id are classified as "Ungrouped"
	if len(result) != 1 {
		t.Fatalf("Expected 1 status group, got %d", len(result))
	}

	statusGroup := result[0]
	if statusGroup.Name != "Ungrouped" {
		t.Errorf("Expected status name 'Ungrouped', got '%s'", statusGroup.Name)
	}
	if statusGroup.Count != 2 {
		t.Errorf("Expected count 2, got %d", statusGroup.Count)
	}
	if len(statusGroup.Features) != 0 {
		t.Errorf("Expected 0 features, got %d", len(statusGroup.Features))
	}
	if statusGroup.Ungrouped == nil {
		t.Fatal("Expected Ungrouped group to exist")
	}
	if len(statusGroup.Ungrouped.Tasks) != 2 {
		t.Errorf("Expected 2 ungrouped tasks, got %d", len(statusGroup.Ungrouped.Tasks))
	}
}

// TestGroupTasksByStatusAndFeature_MultipleStatusesWithFeatures tests full nested grouping.
func TestGroupTasksByStatusAndFeature_MultipleStatusesWithFeatures(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Auth Login", Classification: "ready", Status: "pending", FeatureID: "auth-system", FeaturePriority: "high", Priority: "high"},
		{ID: "task2", Title: "Auth Logout", Classification: "ready", Status: "pending", FeatureID: "auth-system", FeaturePriority: "high", Priority: "high"},
		{ID: "task3", Title: "UI Header", Classification: "ready", Status: "pending", FeatureID: "ui-redesign", FeaturePriority: "medium", Priority: "medium"},
		{ID: "task4", Title: "Standalone", Classification: "ready", Status: "pending", Priority: "low"},
		{ID: "task5", Title: "Auth Middleware", Classification: "waiting", Status: "waiting", FeatureID: "auth-system", FeaturePriority: "high", Priority: "high"},
		{ID: "task6", Title: "Completed Auth", Status: "completed", FeatureID: "auth-system", FeaturePriority: "high", Priority: "high"},
		{ID: "task7", Title: "Completed UI", Status: "completed", FeatureID: "ui-redesign", FeaturePriority: "medium", Priority: "medium"},
	}

	result := GroupTasksByStatusAndFeature(tasks, nil)

	// Verify 4 status groups: Ungrouped (1 task4), Ready (3 with features), Waiting (1), Completed (2)
	if len(result) != 4 {
		t.Fatalf("Expected 4 status groups, got %d", len(result))
	}

	// Check Ungrouped group (task4 without feature)
	ungroupedGroup := result[0]
	if ungroupedGroup.Name != "Ungrouped" {
		t.Errorf("Expected first group 'Ungrouped', got '%s'", ungroupedGroup.Name)
	}
	if ungroupedGroup.Count != 1 {
		t.Errorf("Expected Ungrouped count 1, got %d", ungroupedGroup.Count)
	}

	// Check Ready group (tasks 1, 2, 3 with features)
	readyGroup := result[1]
	if readyGroup.Name != "Ready" {
		t.Errorf("Expected second group 'Ready', got '%s'", readyGroup.Name)
	}
	if readyGroup.Count != 3 {
		t.Errorf("Expected Ready count 3, got %d", readyGroup.Count)
	}
	if len(readyGroup.Features) != 2 {
		t.Fatalf("Expected 2 features in Ready, got %d", len(readyGroup.Features))
	}

	// Features should be sorted by priority: auth-system (high) before ui-redesign (medium)
	if readyGroup.Features[0].ID != "auth-system" {
		t.Errorf("Expected first feature 'auth-system', got '%s'", readyGroup.Features[0].ID)
	}
	if len(readyGroup.Features[0].Tasks) != 2 {
		t.Errorf("Expected 2 tasks in auth-system, got %d", len(readyGroup.Features[0].Tasks))
	}

	if readyGroup.Features[1].ID != "ui-redesign" {
		t.Errorf("Expected second feature 'ui-redesign', got '%s'", readyGroup.Features[1].ID)
	}
	if len(readyGroup.Features[1].Tasks) != 1 {
		t.Errorf("Expected 1 task in ui-redesign, got %d", len(readyGroup.Features[1].Tasks))
	}

	// No ungrouped in Ready anymore - task4 is in top-level Ungrouped
	if readyGroup.Ungrouped != nil {
		t.Errorf("Expected no Ungrouped in Ready, but it exists with %d tasks", len(readyGroup.Ungrouped.Tasks))
	}

	// Check Waiting group
	waitingGroup := result[2]
	if waitingGroup.Name != "Waiting" {
		t.Errorf("Expected third group 'Waiting', got '%s'", waitingGroup.Name)
	}
	if waitingGroup.Count != 1 {
		t.Errorf("Expected Waiting count 1, got %d", waitingGroup.Count)
	}
	if len(waitingGroup.Features) != 1 {
		t.Errorf("Expected 1 feature in Waiting, got %d", len(waitingGroup.Features))
	}
	if waitingGroup.Features[0].ID != "auth-system" {
		t.Errorf("Expected feature 'auth-system' in Waiting, got '%s'", waitingGroup.Features[0].ID)
	}

	// Check Completed group
	completedGroup := result[3]
	if completedGroup.Name != "Completed" {
		t.Errorf("Expected fourth group 'Completed', got '%s'", completedGroup.Name)
	}
	if completedGroup.Count != 2 {
		t.Errorf("Expected Completed count 2, got %d", completedGroup.Count)
	}
	if len(completedGroup.Features) != 2 {
		t.Errorf("Expected 2 features in Completed, got %d", len(completedGroup.Features))
	}
}

// TestGroupTasksByStatusAndFeature_StatusOrder tests that status groups appear in fixed order.
func TestGroupTasksByStatusAndFeature_StatusOrder(t *testing.T) {
	// Create tasks in reverse order to test sorting - all with feature_id to stay in their classification groups
	tasks := []types.ResolvedTask{
		{ID: "task1", Status: "archived", FeatureID: "feature-1"},
		{ID: "task2", Status: "completed", FeatureID: "feature-1"},
		{ID: "task3", Status: "cancelled", FeatureID: "feature-1"},
		{ID: "task4", Status: "blocked", Classification: "blocked", FeatureID: "feature-1"},
		{ID: "task5", Status: "in_progress", Classification: "ready", FeatureID: "feature-1"}, // in_progress stays in classification group
		{ID: "task6", Status: "waiting", Classification: "waiting", FeatureID: "feature-1"},
		{ID: "task7", Status: "pending", Classification: "ready", FeatureID: "feature-1"},
	}

	result := GroupTasksByStatusAndFeature(tasks, nil)

	expectedOrder := []string{"Ready", "Waiting", "Blocked", "Cancelled", "Completed", "Archived"}
	if len(result) != len(expectedOrder) {
		t.Fatalf("Expected %d groups, got %d", len(expectedOrder), len(result))
	}

	for i, expected := range expectedOrder {
		if result[i].Name != expected {
			t.Errorf("Expected group[%d] = '%s', got '%s'", i, expected, result[i].Name)
		}
	}

	// Verify in_progress task is in Ready group
	readyGroup := result[0]
	if readyGroup.Name != "Ready" {
		t.Fatalf("Expected first group to be Ready, got %s", readyGroup.Name)
	}
	if readyGroup.Count != 2 { // task5 (in_progress) and task7 (pending) both in Ready
		t.Errorf("Expected Ready group to have 2 tasks, got %d", readyGroup.Count)
	}
}

// TestGroupTasksByStatusAndFeature_FeaturePrioritySorting tests that features are sorted by priority within status.
func TestGroupTasksByStatusAndFeature_FeaturePrioritySorting(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Classification: "ready", Status: "pending", FeatureID: "feature-low", FeaturePriority: "low"},
		{ID: "task2", Classification: "ready", Status: "pending", FeatureID: "feature-high", FeaturePriority: "high"},
		{ID: "task3", Classification: "ready", Status: "pending", FeatureID: "feature-medium", FeaturePriority: "medium"},
	}

	result := GroupTasksByStatusAndFeature(tasks, nil)

	if len(result) != 1 {
		t.Fatalf("Expected 1 status group, got %d", len(result))
	}

	features := result[0].Features
	if len(features) != 3 {
		t.Fatalf("Expected 3 features, got %d", len(features))
	}

	// High > Medium > Low
	expectedFeatureOrder := []string{"feature-high", "feature-medium", "feature-low"}
	for i, expected := range expectedFeatureOrder {
		if features[i].ID != expected {
			t.Errorf("Expected feature[%d] = '%s', got '%s'", i, expected, features[i].ID)
		}
	}
}

// TestGroupTasksByStatusAndFeature_UngroupedPlacement tests that ungrouped tasks are separated correctly.
func TestGroupTasksByStatusAndFeature_UngroupedPlacement(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Classification: "ready", FeatureID: "feature-a"},
		{ID: "task2", Classification: "ready"}, // No feature_id - goes to Ungrouped status
		{ID: "task3", Status: "completed", FeatureID: "feature-b"},
		{ID: "task4", Status: "completed"}, // No feature_id - completed is terminal, stays in Completed
	}

	result := GroupTasksByStatusAndFeature(tasks, nil)

	// Expect 3 status groups: Ungrouped (task2), Ready (task1), Completed (task3, task4)
	if len(result) != 3 {
		t.Fatalf("Expected 3 status groups, got %d", len(result))
	}

	// Ungrouped group (task2)
	if result[0].Name != "Ungrouped" {
		t.Errorf("Expected first group 'Ungrouped', got '%s'", result[0].Name)
	}
	if result[0].Ungrouped == nil {
		t.Fatal("Expected Ungrouped group to have Ungrouped feature group")
	}
	if len(result[0].Ungrouped.Tasks) != 1 {
		t.Errorf("Expected 1 ungrouped task in Ungrouped status, got %d", len(result[0].Ungrouped.Tasks))
	}

	// Ready group (task1)
	if result[1].Name != "Ready" {
		t.Errorf("Expected second group 'Ready', got '%s'", result[1].Name)
	}

	// Completed group (task3 with feature, task4 without)
	if result[2].Name != "Completed" {
		t.Errorf("Expected third group 'Completed', got '%s'", result[2].Name)
	}
	if result[2].Ungrouped == nil {
		t.Fatal("Expected Ungrouped in Completed")
	}
	if len(result[2].Ungrouped.Tasks) != 1 {
		t.Errorf("Expected 1 ungrouped task in Completed, got %d", len(result[2].Ungrouped.Tasks))
	}
}

// TestGroupTasksByStatusAndFeature_NoUngrouped tests that Ungrouped is nil when all tasks have features.
func TestGroupTasksByStatusAndFeature_NoUngrouped(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Classification: "ready", FeatureID: "feature-a"},
		{ID: "task2", Classification: "ready", FeatureID: "feature-b"},
	}

	result := GroupTasksByStatusAndFeature(tasks, nil)

	if len(result) != 1 {
		t.Fatalf("Expected 1 status group, got %d", len(result))
	}

	if result[0].Ungrouped != nil {
		t.Error("Expected Ungrouped to be nil when all tasks have features")
	}
}

// TestGroupTasksByStatusAndFeature_VisibilityFiltering tests that status groups are filtered by visibility settings.
func TestGroupTasksByStatusAndFeature_VisibilityFiltering(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Status: "draft", FeatureID: "feature-1"},
		{ID: "task2", Status: "draft", FeatureID: "feature-1"},
		{ID: "task3", Status: "draft", FeatureID: "feature-1"},
		{ID: "task4", Classification: "ready", Status: "pending", FeatureID: "feature-2"},
		{ID: "task5", Status: "completed", FeatureID: "feature-3"},
		{ID: "task6", Status: "cancelled", FeatureID: "feature-4"},
	}

	// Test with nil visibleGroups - should show all groups
	resultAll := GroupTasksByStatusAndFeature(tasks, nil)
	if len(resultAll) != 4 {
		t.Errorf("Expected 4 groups with nil visibility, got %d", len(resultAll))
	}

	// Test with empty visibleGroups - should show all groups
	resultEmpty := GroupTasksByStatusAndFeature(tasks, map[string]bool{})
	if len(resultEmpty) != 4 {
		t.Errorf("Expected 4 groups with empty visibility, got %d", len(resultEmpty))
	}

	// Test with Draft hidden
	visibleNoDraft := map[string]bool{
		"Ready":     true,
		"Completed": true,
		"Cancelled": true,
		"Draft":     false,
	}
	resultNoDraft := GroupTasksByStatusAndFeature(tasks, visibleNoDraft)
	if len(resultNoDraft) != 3 {
		t.Errorf("Expected 3 groups with Draft hidden, got %d", len(resultNoDraft))
	}
	// Verify Draft group is not present
	for _, group := range resultNoDraft {
		if group.Name == "Draft" {
			t.Error("Draft group should be hidden but is present")
		}
	}

	// Test with only Ready and Completed visible
	visibleOnlyReadyCompleted := map[string]bool{
		"Ready":     true,
		"Completed": true,
		"Draft":     false,
		"Cancelled": false,
	}
	resultOnlyTwo := GroupTasksByStatusAndFeature(tasks, visibleOnlyReadyCompleted)
	if len(resultOnlyTwo) != 2 {
		t.Errorf("Expected 2 groups with only Ready and Completed visible, got %d", len(resultOnlyTwo))
	}
	// Verify only Ready and Completed are present
	groupNames := make(map[string]bool)
	for _, group := range resultOnlyTwo {
		groupNames[group.Name] = true
	}
	if !groupNames["Ready"] {
		t.Error("Ready group should be visible")
	}
	if !groupNames["Completed"] {
		t.Error("Completed group should be visible")
	}
	if groupNames["Draft"] {
		t.Error("Draft group should be hidden")
	}
	if groupNames["Cancelled"] {
		t.Error("Cancelled group should be hidden")
	}

	// Test with all groups explicitly visible
	visibleAll := map[string]bool{
		"Ready":     true,
		"Draft":     true,
		"Completed": true,
		"Cancelled": true,
	}
	resultExplicitAll := GroupTasksByStatusAndFeature(tasks, visibleAll)
	if len(resultExplicitAll) != 4 {
		t.Errorf("Expected 4 groups with all explicitly visible, got %d", len(resultExplicitAll))
	}
}
