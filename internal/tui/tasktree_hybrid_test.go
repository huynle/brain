package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestTaskTree_HybridView_SeparatesDraftAndCompleted verifies that the hybrid view
// separates draft and completed tasks into their own status groups below features.
func TestTaskTree_HybridView_SeparatesDraftAndCompleted(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Active in Feature 1", Status: "pending", FeatureID: "feature-1"},
		{ID: "task2", Title: "Draft in Feature 1", Status: "draft", FeatureID: "feature-1"},
		{ID: "task3", Title: "Completed in Feature 1", Status: "completed", FeatureID: "feature-1"},
		{ID: "task4", Title: "In Progress Feature 2", Status: "in_progress", FeatureID: "feature-2"},
		{ID: "task5", Title: "Validated in Feature 2", Status: "validated", FeatureID: "feature-2"},
		{ID: "task6", Title: "Ungrouped Active", Status: "pending", FeatureID: ""},
		{ID: "task7", Title: "Ungrouped Draft", Status: "draft", FeatureID: ""},
		{ID: "task8", Title: "Ungrouped Completed", Status: "completed", FeatureID: ""},
	}

	tt := NewTaskTree()
	tt.SetTasks(tasks)

	view := tt.ViewWithProject(100, 50, "test-project")

	// Check that feature sections exist
	if !strings.Contains(view, "Feature: feature-1") {
		t.Error("Expected 'Feature: feature-1' in view")
	}
	if !strings.Contains(view, "Feature: feature-2") {
		t.Error("Expected 'Feature: feature-2' in view")
	}

	// Check that ungrouped section exists
	if !strings.Contains(view, "[Ungrouped]") {
		t.Error("Expected '[Ungrouped]' section in view")
	}

	// Check that Draft status group exists
	if !strings.Contains(view, "Draft (2)") {
		t.Error("Expected 'Draft (2)' status group in view")
	}

	// Check that Completed status group exists
	if !strings.Contains(view, "Completed (3)") {
		t.Error("Expected 'Completed (3)' status group in view")
	}

	// Verify that features show completion stats [completed/total complete]
	// Feature 1 has 3 tasks total (1 completed), so should show [1/3 complete]
	if !strings.Contains(view, "[1/3 complete]") {
		t.Errorf("Expected '[1/3 complete]' for feature-1, got view:\n%s", view)
	}

	// Feature 2 has 2 tasks total (1 validated), so should show [1/2 complete]
	if !strings.Contains(view, "[1/2 complete]") {
		t.Errorf("Expected '[1/2 complete]' for feature-2, got view:\n%s", view)
	}

	// Ungrouped should show (1) not (3)
	if !strings.Contains(view, "[Ungrouped] (1)") {
		t.Errorf("Expected '[Ungrouped] (1)' (only active task), got view:\n%s", view)
	}
}

// TestTaskTree_HybridView_AllActiveTasks verifies that when all tasks are active,
// no status groups are shown.
func TestTaskTree_HybridView_AllActiveTasks(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Active 1", Status: "pending", FeatureID: "feature-1"},
		{ID: "task2", Title: "Active 2", Status: "in_progress", FeatureID: "feature-1"},
		{ID: "task3", Title: "Active 3", Status: "blocked", FeatureID: "feature-2"},
	}

	tt := NewTaskTree()
	tt.SetTasks(tasks)

	view := tt.ViewWithProject(100, 50, "test-project")

	// Check that features exist
	if !strings.Contains(view, "Feature: feature-1") {
		t.Error("Expected 'Feature: feature-1' in view")
	}

	// Check that NO status groups exist
	if strings.Contains(view, "Draft") {
		t.Error("Did not expect 'Draft' section when all tasks are active")
	}
	if strings.Contains(view, "Inactive") {
		t.Error("Did not expect 'Inactive' section when all tasks are active")
	}
}

// TestTaskTree_HybridView_CollapseStates verifies that collapse indicators work correctly.
func TestTaskTree_HybridView_CollapseStates(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Draft Task", Status: "draft", FeatureID: ""},
		{ID: "task2", Title: "Completed Task", Status: "completed", FeatureID: ""},
	}

	tt := NewTaskTree()
	tt.SetTasks(tasks)

	// Draft should be expanded by default (▾)
	view := tt.ViewWithProject(100, 50, "test-project")
	if !strings.Contains(view, "▾ Draft") {
		t.Error("Expected Draft section to be expanded (▾) by default")
	}

	// Completed should be collapsed by default (▶)
	if !strings.Contains(view, "▶ Completed") {
		t.Error("Expected Completed section to be collapsed (▶) by default")
	}
}

// TestTaskTree_HybridView_UngroupedCollapsibleInDraftCompleted verifies that [Ungrouped]
// appears as a collapsible sub-feature header with ▶/▾ in Draft and Completed sections,
// matching the behavior of named sub-features.
func TestTaskTree_HybridView_UngroupedCollapsibleInDraftCompleted(t *testing.T) {
	tasks := []types.ResolvedTask{
		// Active tasks (won't appear in draft/completed)
		{ID: "task1", Title: "Active Task", Status: "pending", FeatureID: "feature-1"},
		// Draft tasks: one in feature, one ungrouped
		{ID: "task2", Title: "Draft in Feature", Status: "draft", FeatureID: "feature-1"},
		{ID: "task3", Title: "Draft Ungrouped", Status: "draft", FeatureID: ""},
		// Completed tasks: one in feature, one ungrouped
		{ID: "task4", Title: "Completed in Feature", Status: "completed", FeatureID: "feature-1"},
		{ID: "task5", Title: "Completed Ungrouped", Status: "completed", FeatureID: ""},
	}

	tt := NewTaskTree()
	tt.SetTasks(tasks)

	// Render with expanded draft, expanded completed
	tt.draftCollapsed = false
	tt.completedCollapsed = false
	view := tt.ViewWithProject(120, 50, "test-project")

	// [Ungrouped] should appear as a sub-feature header in Draft section with collapse arrow
	if !strings.Contains(view, "[Ungrouped]") {
		t.Errorf("Expected '[Ungrouped]' sub-feature header, got view:\n%s", view)
	}

	// Verify [Ungrouped] in Draft section has collapse indicator (▾ when expanded)
	// Both draft and completed ungrouped start expanded by default
	draftUngroupedExpanded := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "[Ungrouped]") && strings.Contains(line, "▾") {
			draftUngroupedExpanded = true
			break
		}
	}
	if !draftUngroupedExpanded {
		t.Errorf("Expected [Ungrouped] sub-feature to have ▾ collapse indicator, got view:\n%s", view)
	}

	// Verify both ungrouped tasks are visible when expanded
	if !strings.Contains(view, "Draft Ungrouped") {
		t.Errorf("Expected 'Draft Ungrouped' task to be visible when [Ungrouped] expanded, got view:\n%s", view)
	}

	// Now collapse the [Ungrouped] in draft section
	tt.featureCollapsed["draft:[Ungrouped]"] = true
	tt.SetTasks(tasks) // re-compute
	tt.draftCollapsed = false
	tt.completedCollapsed = false
	view = tt.ViewWithProject(120, 50, "test-project")

	// Should show ▶ for collapsed [Ungrouped]
	draftUngroupedCollapsed := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "[Ungrouped]") && strings.Contains(line, "▶") {
			draftUngroupedCollapsed = true
			break
		}
	}
	if !draftUngroupedCollapsed {
		t.Errorf("Expected collapsed [Ungrouped] to show ▶ indicator, got view:\n%s", view)
	}

	// The task under collapsed [Ungrouped] in draft should be hidden
	// Count occurrences of "Draft Ungrouped" — it should NOT appear
	draftUngroupedVisible := strings.Contains(view, "Draft Ungrouped")
	if draftUngroupedVisible {
		t.Errorf("Expected 'Draft Ungrouped' task to be hidden when [Ungrouped] collapsed, got view:\n%s", view)
	}
}

// TestTaskTree_HybridView_UngroupedToggleCollapse verifies that Space toggles collapse
// on [Ungrouped] sub-feature headers in Draft/Completed sections.
func TestTaskTree_HybridView_UngroupedToggleCollapse(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Draft Ungrouped 1", Status: "draft", FeatureID: ""},
		{ID: "task2", Title: "Draft Ungrouped 2", Status: "draft", FeatureID: ""},
	}

	tt := NewTaskTree()
	tt.SetTasks(tasks)

	// Navigate to the draft section, then to the [Ungrouped] sub-feature header
	tt.isOnDraftSection = true
	tt.isOnCompletedSection = false
	tt.isOnUngrouped = false
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.draftFeatureIdx = 0 // First (and only) sub-feature: [Ungrouped]
	tt.draftFeatureIDs = []string{"[Ungrouped]"}

	// Toggle collapse on [Ungrouped] in Draft
	tt.ToggleCollapse()

	// Verify the collapse state was set
	if !tt.featureCollapsed["draft:[Ungrouped]"] {
		t.Error("Expected draft:[Ungrouped] to be collapsed after toggle")
	}

	// Toggle again to expand
	tt.ToggleCollapse()

	if tt.featureCollapsed["draft:[Ungrouped]"] {
		t.Error("Expected draft:[Ungrouped] to be expanded after second toggle")
	}
}
