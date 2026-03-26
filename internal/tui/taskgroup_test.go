package tui

import (
	"fmt"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// normalizeClassification Tests
// =============================================================================

func TestNormalizeClassification_Ready(t *testing.T) {
	tests := []struct {
		name           string
		classification string
		status         string
		want           string
	}{
		{
			name:           "classification ready",
			classification: "ready",
			status:         "pending",
			want:           "Ready",
		},
		{
			name:           "status pending",
			classification: "",
			status:         "pending",
			want:           "Ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use feature_id="feat-1" to test WITH feature_id (should remain Ready)
			got := normalizeClassification(tt.classification, tt.status, "feat-1")
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q, \"feat-1\") = %q, want %q", tt.classification, tt.status, got, tt.want)
			}
		})
	}
}

func TestNormalizeClassification_Waiting(t *testing.T) {
	tests := []struct {
		name           string
		classification string
		status         string
		want           string
	}{
		{
			name:           "classification waiting",
			classification: "waiting",
			status:         "pending",
			want:           "Waiting",
		},
		{
			name:           "status waiting",
			classification: "",
			status:         "waiting",
			want:           "Waiting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use feature_id="feat-1" to test WITH feature_id (should remain Waiting)
			got := normalizeClassification(tt.classification, tt.status, "feat-1")
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q, \"feat-1\") = %q, want %q", tt.classification, tt.status, got, tt.want)
			}
		})
	}
}

func TestNormalizeClassification_InProgress(t *testing.T) {
	// Test that in_progress tasks stay in their classification groups
	// They don't get moved to a separate "Active" group anymore
	tests := []struct {
		name           string
		classification string
		status         string
		want           string
	}{
		{
			name:           "in_progress with ready classification",
			classification: "ready",
			status:         "in_progress",
			want:           "Ready",
		},
		{
			name:           "in_progress with waiting classification",
			classification: "waiting",
			status:         "in_progress",
			want:           "Waiting",
		},
		{
			name:           "in_progress with blocked classification",
			classification: "blocked",
			status:         "in_progress",
			want:           "Blocked",
		},
		{
			name:           "in_progress with no classification defaults to Inactive",
			classification: "",
			status:         "in_progress",
			want:           "Inactive",
		},
		{
			name:           "active status with ready classification",
			classification: "ready",
			status:         "active",
			want:           "Ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use feature_id="feat-1" to test WITH feature_id
			got := normalizeClassification(tt.classification, tt.status, "feat-1")
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q, \"feat-1\") = %q, want %q", tt.classification, tt.status, got, tt.want)
			}
		})
	}
}

func TestNormalizeClassification_Blocked(t *testing.T) {
	tests := []struct {
		name           string
		classification string
		status         string
		want           string
	}{
		{
			name:           "classification blocked",
			classification: "blocked",
			status:         "pending",
			want:           "Blocked",
		},
		{
			name:           "status blocked",
			classification: "",
			status:         "blocked",
			want:           "Blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use feature_id="feat-1" to test WITH feature_id
			got := normalizeClassification(tt.classification, tt.status, "feat-1")
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q, \"feat-1\") = %q, want %q", tt.classification, tt.status, got, tt.want)
			}
		})
	}
}

// NEW: Test cases for split terminal states
func TestNormalizeClassification_Draft(t *testing.T) {
	// Terminal states (draft, completed, etc.) should not be affected by feature_id
	got := normalizeClassification("", "draft", "")
	want := "Draft"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"draft\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Completed(t *testing.T) {
	got := normalizeClassification("", "completed", "")
	want := "Inactive"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"completed\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Validated(t *testing.T) {
	got := normalizeClassification("", "validated", "")
	want := "Inactive"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"validated\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Cancelled(t *testing.T) {
	got := normalizeClassification("", "cancelled", "")
	want := "Inactive"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"cancelled\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Superseded(t *testing.T) {
	got := normalizeClassification("", "superseded", "")
	want := "Inactive"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"superseded\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Archived(t *testing.T) {
	got := normalizeClassification("", "archived", "")
	want := "Inactive"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"archived\", \"\") = %q, want %q", got, want)
	}
}

// Test that ALL inactive statuses map to the single "Inactive" group
func TestNormalizeClassification_AllInactiveStatuses(t *testing.T) {
	inactiveStatuses := []string{"completed", "validated", "cancelled", "superseded", "archived"}
	for _, status := range inactiveStatuses {
		got := normalizeClassification("", status, "")
		if got != "Inactive" {
			t.Errorf("normalizeClassification(\"\", %q, \"\") = %q, want \"Inactive\"", status, got)
		}
		// Also test with feature_id
		got = normalizeClassification("", status, "feature-1")
		if got != "Inactive" {
			t.Errorf("normalizeClassification(\"\", %q, \"feature-1\") = %q, want \"Inactive\"", status, got)
		}
	}
}

// Test that IsInactiveStatus helper works correctly
func TestIsInactiveStatus(t *testing.T) {
	inactive := []string{"completed", "validated", "cancelled", "superseded", "archived"}
	for _, s := range inactive {
		if !IsInactiveStatus(s) {
			t.Errorf("IsInactiveStatus(%q) = false, want true", s)
		}
	}
	active := []string{"draft", "pending", "active", "in_progress", "blocked"}
	for _, s := range active {
		if IsInactiveStatus(s) {
			t.Errorf("IsInactiveStatus(%q) = true, want false", s)
		}
	}
}

// Test that tasks without feature_id are classified as "Ungrouped"
func TestNormalizeClassification_Ungrouped(t *testing.T) {
	tests := []struct {
		name           string
		classification string
		status         string
		featureID      string
		want           string
	}{
		{
			name:           "ready classification without feature_id",
			classification: "ready",
			status:         "pending",
			featureID:      "",
			want:           "Ungrouped",
		},
		{
			name:           "pending status without feature_id",
			classification: "",
			status:         "pending",
			featureID:      "",
			want:           "Ungrouped",
		},
		{
			name:           "waiting classification without feature_id",
			classification: "waiting",
			status:         "pending",
			featureID:      "",
			want:           "Ungrouped",
		},
		{
			name:           "blocked classification without feature_id",
			classification: "blocked",
			status:         "pending",
			featureID:      "",
			want:           "Ungrouped",
		},
		{
			name:           "ready classification WITH feature_id stays Ready",
			classification: "ready",
			status:         "pending",
			featureID:      "feature-123",
			want:           "Ready",
		},
		{
			name:           "waiting classification WITH feature_id stays Waiting",
			classification: "waiting",
			status:         "pending",
			featureID:      "feature-123",
			want:           "Waiting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeClassification(tt.classification, tt.status, tt.featureID)
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q, %q) = %q, want %q",
					tt.classification, tt.status, tt.featureID, got, tt.want)
			}
		})
	}
}

// =============================================================================
// GroupTasks Tests
// =============================================================================

func TestGroupTasks_TerminalStatesInInactiveGroup(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Draft task", Status: "draft", Priority: "medium", Classification: ""},
		{ID: "t2", Title: "Completed task", Status: "completed", Priority: "medium", Classification: ""},
		{ID: "t3", Title: "Validated task", Status: "validated", Priority: "medium", Classification: ""},
		{ID: "t4", Title: "Cancelled task", Status: "cancelled", Priority: "medium", Classification: ""},
		{ID: "t5", Title: "Superseded task", Status: "superseded", Priority: "medium", Classification: ""},
		{ID: "t6", Title: "Archived task", Status: "archived", Priority: "medium", Classification: ""},
	}

	groups := GroupTasks(tasks, nil)

	// Should have 2 groups: Draft (1 task) and Inactive (5 tasks)
	if len(groups) != 2 {
		names := make([]string, len(groups))
		for i, g := range groups {
			names[i] = fmt.Sprintf("%s(%d)", g.Name, g.Count)
		}
		t.Fatalf("expected 2 groups, got %d: %v", len(groups), names)
	}

	if groups[0].Name != "Draft" {
		t.Errorf("group[0] name = %q, want %q", groups[0].Name, "Draft")
	}
	if groups[0].Count != 1 {
		t.Errorf("group[0] count = %d, want 1", groups[0].Count)
	}
	if groups[1].Name != "Inactive" {
		t.Errorf("group[1] name = %q, want %q", groups[1].Name, "Inactive")
	}
	if groups[1].Count != 5 {
		t.Errorf("group[1] count = %d, want 5", groups[1].Count)
	}
}

func TestGroupTasks_AllGroupsInCorrectOrder(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Ready", Status: "pending", Priority: "high", Classification: "ready", FeatureID: "feature-1"},
		{ID: "t2", Title: "Waiting", Status: "pending", Priority: "high", Classification: "waiting", FeatureID: "feature-1"},
		{ID: "t3", Title: "Active", Status: "in_progress", Priority: "high", Classification: "ready", FeatureID: "feature-1"}, // in_progress stays in classification group
		{ID: "t4", Title: "Blocked", Status: "blocked", Priority: "high", Classification: "blocked", FeatureID: "feature-1"},
		{ID: "t5", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
		{ID: "t6", Title: "Cancelled", Status: "cancelled", Priority: "high", Classification: ""},
		{ID: "t7", Title: "Completed", Status: "completed", Priority: "high", Classification: ""},
		{ID: "t8", Title: "Validated", Status: "validated", Priority: "high", Classification: ""},
		{ID: "t9", Title: "Superseded", Status: "superseded", Priority: "high", Classification: ""},
		{ID: "t10", Title: "Archived", Status: "archived", Priority: "high", Classification: ""},
	}

	groups := GroupTasks(tasks, nil)

	// Should have 5 groups: Ready, Waiting, Blocked, Draft, Inactive
	expectedOrder := []string{"Ready", "Waiting", "Blocked", "Draft", "Inactive"}

	if len(groups) != len(expectedOrder) {
		names := make([]string, len(groups))
		for i, g := range groups {
			names[i] = fmt.Sprintf("%s(%d)", g.Name, g.Count)
		}
		t.Fatalf("expected %d groups, got %d: %v", len(expectedOrder), len(groups), names)
	}

	for i, expected := range expectedOrder {
		if groups[i].Name != expected {
			t.Errorf("group[%d] name = %q, want %q", i, groups[i].Name, expected)
		}
	}

	// Verify in_progress task is in Ready group (based on classification)
	readyGroup := groups[0]
	if readyGroup.Name != "Ready" {
		t.Fatalf("expected first group to be Ready, got %s", readyGroup.Name)
	}
	foundActive := false
	for _, task := range readyGroup.Tasks {
		if task.ID == "t3" && task.Status == "in_progress" {
			foundActive = true
			break
		}
	}
	if !foundActive {
		t.Errorf("expected in_progress task t3 to be in Ready group")
	}

	// Verify Inactive group contains all terminal statuses
	inactiveGroup := groups[4]
	if inactiveGroup.Name != "Inactive" {
		t.Fatalf("expected last group to be Inactive, got %s", inactiveGroup.Name)
	}
	if inactiveGroup.Count != 5 {
		t.Errorf("expected Inactive group to have 5 tasks, got %d", inactiveGroup.Count)
	}
	statusSet := make(map[string]bool)
	for _, task := range inactiveGroup.Tasks {
		statusSet[task.Status] = true
	}
	for _, expected := range []string{"cancelled", "completed", "validated", "superseded", "archived"} {
		if !statusSet[expected] {
			t.Errorf("expected Inactive group to contain status %q", expected)
		}
	}
}

// Phase 3: Visibility filtering tests (CORRECTED)

func TestGroupTasks_WithVisibility_HidesInvisibleGroups(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Ready", Status: "pending", Priority: "high", Classification: "ready", FeatureID: "feature-1"},
		{ID: "t2", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
		{ID: "t3", Title: "Completed", Status: "completed", Priority: "high", Classification: ""},
		{ID: "t4", Title: "Archived", Status: "archived", Priority: "high", Classification: ""},
	}

	// Hide Draft and Inactive
	visibleGroups := map[string]bool{
		"Ready":    true,
		"Inactive": false,
		"Draft":    false,
	}

	groups := GroupTasks(tasks, visibleGroups)

	// Should only have 1 group: Ready (Draft hidden, Inactive hidden)
	if len(groups) != 1 {
		names := make([]string, len(groups))
		for i, g := range groups {
			names[i] = g.Name
		}
		t.Fatalf("expected 1 group, got %d: %v", len(groups), names)
	}

	if groups[0].Name != "Ready" {
		t.Errorf("group[0] name = %q, want %q", groups[0].Name, "Ready")
	}
}

func TestGroupTasks_WithVisibility_NilMapShowsAll(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Ready", Status: "pending", Priority: "high", Classification: "ready", FeatureID: "feature-1"},
		{ID: "t2", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
		{ID: "t3", Title: "Archived", Status: "archived", Priority: "high", Classification: ""},
	}

	groups := GroupTasks(tasks, nil)

	// Should have 3 groups: Ready, Draft, Inactive (archived maps to Inactive)
	if len(groups) != 3 {
		names := make([]string, len(groups))
		for i, g := range groups {
			names[i] = g.Name
		}
		t.Fatalf("expected 3 groups, got %d: %v", len(groups), names)
	}

	expectedNames := []string{"Ready", "Draft", "Inactive"}
	for i, expected := range expectedNames {
		if groups[i].Name != expected {
			t.Errorf("group[%d] name = %q, want %q", i, groups[i].Name, expected)
		}
	}
}

func TestGroupTasks_WithVisibility_EmptyMapShowsAll(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Ready", Status: "pending", Priority: "high", Classification: "ready"},
		{ID: "t2", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
	}

	groups := GroupTasks(tasks, map[string]bool{})

	// Should have all 2 groups when map is empty
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func TestGroupTasks_WithVisibility_OnlyHiddenGroups(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
		{ID: "t2", Title: "Archived", Status: "archived", Priority: "high", Classification: ""},
	}

	visibleGroups := map[string]bool{
		"Draft":    false,
		"Inactive": false,
	}

	groups := GroupTasks(tasks, visibleGroups)

	// Should have 0 groups when all are hidden
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(groups))
	}
}

// Test the specific user request: tasks without feature_id in classification mode should be "Ungrouped"
func TestGroupTasks_UngroupedForTasksWithoutFeature(t *testing.T) {
	tasks := []types.ResolvedTask{
		// Tasks WITH feature_id - should use normal classification
		{ID: "t1", Title: "Ready with feature", Status: "pending", Priority: "high", Classification: "ready", FeatureID: "feature-1"},
		{ID: "t2", Title: "Waiting with feature", Status: "pending", Priority: "high", Classification: "waiting", FeatureID: "feature-1"},
		// Tasks WITHOUT feature_id - should be "Ungrouped"
		{ID: "t3", Title: "Ready without feature", Status: "pending", Priority: "high", Classification: "ready"},
		{ID: "t4", Title: "Waiting without feature", Status: "pending", Priority: "high", Classification: "waiting"},
		// Terminal states without feature_id - should go to Inactive
		{ID: "t5", Title: "Completed without feature", Status: "completed", Priority: "high", Classification: ""},
	}

	groups := GroupTasks(tasks, nil)

	// Should have 4 groups: Ready (1 task), Waiting (1 task), Ungrouped (2 tasks), Inactive (1 task)
	if len(groups) != 4 {
		names := make([]string, len(groups))
		for i, g := range groups {
			names[i] = fmt.Sprintf("%s(%d)", g.Name, g.Count)
		}
		t.Fatalf("expected 4 groups, got %d: %v", len(groups), names)
	}

	// Check order and contents
	expectedGroups := map[string]int{
		"Ready":     1,
		"Waiting":   1,
		"Ungrouped": 2,
		"Inactive":  1,
	}

	for _, group := range groups {
		expectedCount, ok := expectedGroups[group.Name]
		if !ok {
			t.Errorf("unexpected group: %s", group.Name)
			continue
		}
		if group.Count != expectedCount {
			t.Errorf("group %s: expected %d tasks, got %d", group.Name, expectedCount, group.Count)
		}
	}

	// Verify order: Ready, Waiting, Ungrouped, Inactive
	if groups[0].Name != "Ready" {
		t.Errorf("expected first group to be Ready, got %s", groups[0].Name)
	}
	if groups[1].Name != "Waiting" {
		t.Errorf("expected second group to be Waiting, got %s", groups[1].Name)
	}
	if groups[2].Name != "Ungrouped" {
		t.Errorf("expected third group to be Ungrouped, got %s", groups[2].Name)
	}
	if groups[3].Name != "Inactive" {
		t.Errorf("expected fourth group to be Inactive, got %s", groups[3].Name)
	}

	// Verify Ungrouped contains the correct tasks
	ungroupedIDs := make(map[string]bool)
	for _, task := range groups[2].Tasks {
		ungroupedIDs[task.ID] = true
	}
	if !ungroupedIDs["t3"] || !ungroupedIDs["t4"] {
		t.Errorf("Ungrouped should contain t3 and t4, got: %v", ungroupedIDs)
	}
}
