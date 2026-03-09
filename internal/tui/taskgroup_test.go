package tui

import (
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
			name:           "in_progress with no classification defaults to Completed",
			classification: "",
			status:         "in_progress",
			want:           "Completed",
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
	want := "Completed"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"completed\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Validated(t *testing.T) {
	got := normalizeClassification("", "validated", "")
	want := "Validated"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"validated\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Cancelled(t *testing.T) {
	got := normalizeClassification("", "cancelled", "")
	want := "Cancelled"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"cancelled\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Superseded(t *testing.T) {
	got := normalizeClassification("", "superseded", "")
	want := "Superseded"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"superseded\", \"\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Archived(t *testing.T) {
	got := normalizeClassification("", "archived", "")
	want := "Archived"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"archived\", \"\") = %q, want %q", got, want)
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

func TestGroupTasks_TerminalStatesInSeparateGroups(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Draft task", Status: "draft", Priority: "medium", Classification: ""},
		{ID: "t2", Title: "Completed task", Status: "completed", Priority: "medium", Classification: ""},
		{ID: "t3", Title: "Validated task", Status: "validated", Priority: "medium", Classification: ""},
		{ID: "t4", Title: "Cancelled task", Status: "cancelled", Priority: "medium", Classification: ""},
		{ID: "t5", Title: "Superseded task", Status: "superseded", Priority: "medium", Classification: ""},
		{ID: "t6", Title: "Archived task", Status: "archived", Priority: "medium", Classification: ""},
	}

	groups := GroupTasks(tasks, nil)

	// Should have 6 groups: Draft, Cancelled, Completed, Validated, Superseded, Archived
	if len(groups) != 6 {
		t.Fatalf("expected 6 groups, got %d", len(groups))
	}

	// Verify group names in order
	expectedOrder := []string{"Draft", "Cancelled", "Completed", "Validated", "Superseded", "Archived"}
	for i, expected := range expectedOrder {
		if groups[i].Name != expected {
			t.Errorf("group[%d] name = %q, want %q", i, groups[i].Name, expected)
		}
		if groups[i].Count != 1 {
			t.Errorf("group[%d] count = %d, want 1", i, groups[i].Count)
		}
	}
}

func TestGroupTasks_AllGroupsInCorrectOrder(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Ready", Status: "pending", Priority: "high", Classification: "ready"},
		{ID: "t2", Title: "Waiting", Status: "pending", Priority: "high", Classification: "waiting"},
		{ID: "t3", Title: "Active", Status: "in_progress", Priority: "high", Classification: "ready"}, // in_progress stays in classification group
		{ID: "t4", Title: "Blocked", Status: "blocked", Priority: "high", Classification: "blocked"},
		{ID: "t5", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
		{ID: "t6", Title: "Cancelled", Status: "cancelled", Priority: "high", Classification: ""},
		{ID: "t7", Title: "Completed", Status: "completed", Priority: "high", Classification: ""},
		{ID: "t8", Title: "Validated", Status: "validated", Priority: "high", Classification: ""},
		{ID: "t9", Title: "Superseded", Status: "superseded", Priority: "high", Classification: ""},
		{ID: "t10", Title: "Archived", Status: "archived", Priority: "high", Classification: ""},
	}

	groups := GroupTasks(tasks, nil)

	// Should have 9 groups in this specific order (no "Active" group - in_progress tasks stay in their classification groups)
	expectedOrder := []string{"Ready", "Waiting", "Blocked", "Draft", "Cancelled", "Completed", "Validated", "Superseded", "Archived"}

	if len(groups) != len(expectedOrder) {
		t.Fatalf("expected %d groups, got %d", len(expectedOrder), len(groups))
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
}

// Phase 3: Visibility filtering tests (CORRECTED)

func TestGroupTasks_WithVisibility_HidesInvisibleGroups(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Ready", Status: "pending", Priority: "high", Classification: "ready"},
		{ID: "t2", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
		{ID: "t3", Title: "Completed", Status: "completed", Priority: "high", Classification: ""},
		{ID: "t4", Title: "Archived", Status: "archived", Priority: "high", Classification: ""},
	}

	// Hide Draft and Archived
	visibleGroups := map[string]bool{
		"Ready":     true,
		"Completed": true,
		"Draft":     false,
		"Archived":  false,
	}

	groups := GroupTasks(tasks, visibleGroups)

	// Should only have 2 groups: Ready, Completed
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	if groups[0].Name != "Ready" {
		t.Errorf("group[0] name = %q, want %q", groups[0].Name, "Ready")
	}
	if groups[1].Name != "Completed" {
		t.Errorf("group[1].Name = %q, want %q", groups[1].Name, "Completed")
	}
}

func TestGroupTasks_WithVisibility_NilMapShowsAll(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Ready", Status: "pending", Priority: "high", Classification: "ready"},
		{ID: "t2", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
		{ID: "t3", Title: "Archived", Status: "archived", Priority: "high", Classification: ""},
	}

	groups := GroupTasks(tasks, nil)

	// Should have all 3 groups
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	expectedNames := []string{"Ready", "Draft", "Archived"}
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
		"Archived": false,
	}

	groups := GroupTasks(tasks, visibleGroups)

	// Should have 0 groups when all are hidden
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(groups))
	}
}
