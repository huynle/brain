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

// =============================================================================
// filterByExecutors Tests
// =============================================================================

func TestFilterByExecutors_NoFilter(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Executor: "opencode"},
		{ID: "t2", Executor: "pi"},
	}

	// nil executors = return all tasks unchanged
	result := filterByExecutors(tasks, nil)
	if len(result) != 2 {
		t.Errorf("expected 2 tasks for nil filter, got %d", len(result))
	}

	// empty executors = return all tasks unchanged
	result = filterByExecutors(tasks, []string{})
	if len(result) != 2 {
		t.Errorf("expected 2 tasks for empty filter, got %d", len(result))
	}
}

func TestFilterByExecutors_SingleExecutor(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Executor: "opencode"},
		{ID: "t2", Executor: "pi"},
		{ID: "t3", Executor: "opencode"},
	}

	result := filterByExecutors(tasks, []string{"opencode"})
	if len(result) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result))
	}
	ids := make(map[string]bool)
	for _, r := range result {
		ids[r.ID] = true
	}
	if !ids["t1"] || !ids["t3"] {
		t.Errorf("expected tasks t1 and t3, got %v", ids)
	}
}

func TestFilterByExecutors_MultipleExecutors(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Executor: "opencode"},
		{ID: "t2", Executor: "pi"},
		{ID: "t3", Executor: "custom"},
	}

	result := filterByExecutors(tasks, []string{"opencode", "pi"})
	if len(result) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result))
	}
	ids := make(map[string]bool)
	for _, r := range result {
		ids[r.ID] = true
	}
	if !ids["t1"] || !ids["t2"] {
		t.Errorf("expected tasks t1 and t2, got %v", ids)
	}
}

func TestFilterByExecutors_EmptyExecutorDefaultsToOpencode(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Executor: ""}, // empty → treated as "opencode"
		{ID: "t2"},               // zero value → treated as "opencode"
		{ID: "t3", Executor: "pi"},
		{ID: "t4", Executor: "opencode"},
	}

	result := filterByExecutors(tasks, []string{"opencode"})
	if len(result) != 3 {
		t.Fatalf("expected 3 tasks (t1, t2, t4), got %d", len(result))
	}
	ids := make(map[string]bool)
	for _, r := range result {
		ids[r.ID] = true
	}
	for _, expected := range []string{"t1", "t2", "t4"} {
		if !ids[expected] {
			t.Errorf("expected task %s in results (empty executor should default to opencode)", expected)
		}
	}
}

func TestFilterByExecutors_NoMatch(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Executor: "opencode"},
		{ID: "t2", Executor: "opencode"},
	}

	result := filterByExecutors(tasks, []string{"pi"})
	if len(result) != 0 {
		t.Errorf("expected 0 tasks for non-matching executor, got %d", len(result))
	}
}

func TestFilterByExecutors_UnknownExecutor(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Executor: "opencode"},
		{ID: "t2", Executor: "custom-exec"},
	}

	// Requesting an unknown executor type that exists on tasks
	result := filterByExecutors(tasks, []string{"custom-exec"})
	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	if result[0].ID != "t2" {
		t.Errorf("expected task t2, got %s", result[0].ID)
	}
}

func TestFilterByExecutors_NilTasks(t *testing.T) {
	result := filterByExecutors(nil, []string{"opencode"})
	if result != nil {
		t.Errorf("expected nil for nil tasks, got %v", result)
	}
}

func TestFilterByExecutors_PiOnlyRunner(t *testing.T) {
	// Simulates a Pi-only runner that can only run "pi" tasks
	tasks := []types.ResolvedTask{
		{ID: "t1", Executor: "opencode"},
		{ID: "t2", Executor: "pi"},
		{ID: "t3", Executor: ""}, // defaults to opencode
		{ID: "t4", Executor: "pi"},
	}

	result := filterByExecutors(tasks, []string{"pi"})
	if len(result) != 2 {
		t.Fatalf("expected 2 pi tasks, got %d", len(result))
	}
	ids := make(map[string]bool)
	for _, r := range result {
		ids[r.ID] = true
	}
	if !ids["t2"] || !ids["t4"] {
		t.Errorf("expected tasks t2 and t4, got %v", ids)
	}
}
