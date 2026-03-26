package service

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// filterByFeatureIDs Tests
// =============================================================================

func TestFilterByFeatureIDs_EmptyFilter(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", FeatureID: "feat-a"},
		{ID: "t2", FeatureID: "feat-b"},
	}

	result := filterByFeatureIDs(tasks, nil)
	if len(result) != 0 {
		t.Errorf("expected 0 tasks for nil filter, got %d", len(result))
	}
}

func TestFilterByFeatureIDs_EmptySlice(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", FeatureID: "feat-a"},
	}

	result := filterByFeatureIDs(tasks, []string{})
	if len(result) != 0 {
		t.Errorf("expected 0 tasks for empty filter, got %d", len(result))
	}
}

func TestFilterByFeatureIDs_SingleMatch(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", FeatureID: "feat-a"},
		{ID: "t2", FeatureID: "feat-b"},
		{ID: "t3", FeatureID: "feat-c"},
	}

	result := filterByFeatureIDs(tasks, []string{"feat-b"})
	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	if result[0].ID != "t2" {
		t.Errorf("expected task t2, got %s", result[0].ID)
	}
}

func TestFilterByFeatureIDs_MultipleMatch(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", FeatureID: "feat-a"},
		{ID: "t2", FeatureID: "feat-b"},
		{ID: "t3", FeatureID: "feat-c"},
		{ID: "t4", FeatureID: "feat-a"},
	}

	result := filterByFeatureIDs(tasks, []string{"feat-a", "feat-c"})
	if len(result) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(result))
	}

	ids := make(map[string]bool)
	for _, r := range result {
		ids[r.ID] = true
	}
	for _, expected := range []string{"t1", "t3", "t4"} {
		if !ids[expected] {
			t.Errorf("expected task %s in results", expected)
		}
	}
}

func TestFilterByFeatureIDs_NoMatch(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", FeatureID: "feat-a"},
		{ID: "t2", FeatureID: "feat-b"},
	}

	result := filterByFeatureIDs(tasks, []string{"feat-z"})
	if len(result) != 0 {
		t.Errorf("expected 0 tasks for non-matching filter, got %d", len(result))
	}
}

func TestFilterByFeatureIDs_EmptyFeatureID(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", FeatureID: "feat-a"},
		{ID: "t2", FeatureID: ""},
		{ID: "t3"},
	}

	// Tasks without a feature_id should NOT match any filter
	result := filterByFeatureIDs(tasks, []string{"feat-a"})
	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	if result[0].ID != "t1" {
		t.Errorf("expected task t1, got %s", result[0].ID)
	}
}

func TestFilterByFeatureIDs_NilTasks(t *testing.T) {
	result := filterByFeatureIDs(nil, []string{"feat-a"})
	if result != nil {
		t.Errorf("expected nil for nil tasks, got %v", result)
	}
}
