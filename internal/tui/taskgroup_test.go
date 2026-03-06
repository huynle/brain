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
			got := normalizeClassification(tt.classification, tt.status)
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q) = %q, want %q", tt.classification, tt.status, got, tt.want)
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
			got := normalizeClassification(tt.classification, tt.status)
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q) = %q, want %q", tt.classification, tt.status, got, tt.want)
			}
		})
	}
}

func TestNormalizeClassification_Active(t *testing.T) {
	tests := []struct {
		name           string
		classification string
		status         string
		want           string
	}{
		{
			name:           "status in_progress",
			classification: "",
			status:         "in_progress",
			want:           "Active",
		},
		{
			name:           "status active",
			classification: "",
			status:         "active",
			want:           "Active",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeClassification(tt.classification, tt.status)
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q) = %q, want %q", tt.classification, tt.status, got, tt.want)
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
			got := normalizeClassification(tt.classification, tt.status)
			if got != tt.want {
				t.Errorf("normalizeClassification(%q, %q) = %q, want %q", tt.classification, tt.status, got, tt.want)
			}
		})
	}
}

// NEW: Test cases for split terminal states
func TestNormalizeClassification_Draft(t *testing.T) {
	got := normalizeClassification("", "draft")
	want := "Draft"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"draft\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Completed(t *testing.T) {
	got := normalizeClassification("", "completed")
	want := "Completed"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"completed\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Validated(t *testing.T) {
	got := normalizeClassification("", "validated")
	want := "Validated"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"validated\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Cancelled(t *testing.T) {
	got := normalizeClassification("", "cancelled")
	want := "Cancelled"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"cancelled\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Superseded(t *testing.T) {
	got := normalizeClassification("", "superseded")
	want := "Superseded"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"superseded\") = %q, want %q", got, want)
	}
}

func TestNormalizeClassification_Archived(t *testing.T) {
	got := normalizeClassification("", "archived")
	want := "Archived"
	if got != want {
		t.Errorf("normalizeClassification(\"\", \"archived\") = %q, want %q", got, want)
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

	groups := GroupTasks(tasks)

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
		{ID: "t3", Title: "Active", Status: "in_progress", Priority: "high", Classification: ""},
		{ID: "t4", Title: "Blocked", Status: "blocked", Priority: "high", Classification: "blocked"},
		{ID: "t5", Title: "Draft", Status: "draft", Priority: "high", Classification: ""},
		{ID: "t6", Title: "Cancelled", Status: "cancelled", Priority: "high", Classification: ""},
		{ID: "t7", Title: "Completed", Status: "completed", Priority: "high", Classification: ""},
		{ID: "t8", Title: "Validated", Status: "validated", Priority: "high", Classification: ""},
		{ID: "t9", Title: "Superseded", Status: "superseded", Priority: "high", Classification: ""},
		{ID: "t10", Title: "Archived", Status: "archived", Priority: "high", Classification: ""},
	}

	groups := GroupTasks(tasks)

	// Should have 10 groups in this specific order
	expectedOrder := []string{"Ready", "Waiting", "Active", "Blocked", "Draft", "Cancelled", "Completed", "Validated", "Superseded", "Archived"}

	if len(groups) != len(expectedOrder) {
		t.Fatalf("expected %d groups, got %d", len(expectedOrder), len(groups))
	}

	for i, expected := range expectedOrder {
		if groups[i].Name != expected {
			t.Errorf("group[%d] name = %q, want %q", i, groups[i].Name, expected)
		}
	}
}
