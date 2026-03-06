package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// findActiveAncestor Tests - Phase 2: Parent Chain Walking
// =============================================================================

// makeTaskWithParent creates a task with parent_id set.
func makeTaskWithParent(id, title, parentID string) types.ResolvedTask {
	return types.ResolvedTask{
		ID:             id,
		Title:          title,
		ParentID:       parentID,
		Classification: "ready",
		Priority:       "medium",
		Status:         "pending",
	}
}

// TestFindActiveAncestor_DirectActiveParent tests finding a parent that is in the active task map.
func TestFindActiveAncestor_DirectActiveParent(t *testing.T) {
	// Setup: child points to active parent
	parent := makeTaskWithParent("parent", "Parent Task", "")
	child := makeTaskWithParent("child", "Child Task", "parent")

	activeTaskMap := map[string]types.ResolvedTask{
		"parent": parent,
		"child":  child,
	}
	allTaskMap := map[string]types.ResolvedTask{
		"parent": parent,
		"child":  child,
	}

	result := findActiveAncestor("parent", activeTaskMap, allTaskMap)
	if result != "parent" {
		t.Errorf("expected 'parent', got '%s'", result)
	}
}

// TestFindActiveAncestor_SkipCompletedParent tests walking through a completed parent to find active grandparent.
func TestFindActiveAncestor_SkipCompletedParent(t *testing.T) {
	// Setup: child -> completed parent -> active grandparent
	grandparent := makeTaskWithParent("gp", "Grandparent Task", "")
	grandparent.Status = "pending" // active

	parent := makeTaskWithParent("parent", "Parent Task", "gp")
	parent.Status = "completed" // completed, not in activeTaskMap

	child := makeTaskWithParent("child", "Child Task", "parent")

	activeTaskMap := map[string]types.ResolvedTask{
		"gp":    grandparent,
		"child": child,
	}
	allTaskMap := map[string]types.ResolvedTask{
		"gp":     grandparent,
		"parent": parent,
		"child":  child,
	}

	result := findActiveAncestor("parent", activeTaskMap, allTaskMap)
	if result != "gp" {
		t.Errorf("expected 'gp', got '%s'", result)
	}
}

// TestFindActiveAncestor_NoActiveAncestor tests when no active ancestor exists.
func TestFindActiveAncestor_NoActiveAncestor(t *testing.T) {
	// Setup: child -> completed parent -> no more parents
	parent := makeTaskWithParent("parent", "Parent Task", "")
	parent.Status = "completed"

	child := makeTaskWithParent("child", "Child Task", "parent")

	activeTaskMap := map[string]types.ResolvedTask{
		"child": child,
	}
	allTaskMap := map[string]types.ResolvedTask{
		"parent": parent,
		"child":  child,
	}

	result := findActiveAncestor("parent", activeTaskMap, allTaskMap)
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

// TestFindActiveAncestor_CycleDetection tests that cycles in parent_id chain are detected.
func TestFindActiveAncestor_CycleDetection(t *testing.T) {
	// Setup: t1 -> t2 -> t3 -> t1 (cycle)
	t1 := makeTaskWithParent("t1", "Task 1", "t3")
	t2 := makeTaskWithParent("t2", "Task 2", "t1")
	t3 := makeTaskWithParent("t3", "Task 3", "t2")

	activeTaskMap := map[string]types.ResolvedTask{
		"t1": t1,
		"t2": t2,
		"t3": t3,
	}
	allTaskMap := map[string]types.ResolvedTask{
		"t1": t1,
		"t2": t2,
		"t3": t3,
	}

	// Starting from t3's parent (t2), should detect cycle
	result := findActiveAncestor("t2", activeTaskMap, allTaskMap)
	if result != "" {
		t.Errorf("expected empty string for cycle, got '%s'", result)
	}
}

// TestFindActiveAncestor_NonExistentParent tests when parent_id points to non-existent task.
func TestFindActiveAncestor_NonExistentParent(t *testing.T) {
	// Setup: child points to non-existent parent
	child := makeTaskWithParent("child", "Child Task", "missing")

	activeTaskMap := map[string]types.ResolvedTask{
		"child": child,
	}
	allTaskMap := map[string]types.ResolvedTask{
		"child": child,
	}

	result := findActiveAncestor("missing", activeTaskMap, allTaskMap)
	if result != "" {
		t.Errorf("expected empty string for non-existent parent, got '%s'", result)
	}
}

// TestFindActiveAncestor_DeepChain tests walking a deep parent chain (5+ levels).
func TestFindActiveAncestor_DeepChain(t *testing.T) {
	// Setup: t5 -> t4 -> t3 (completed) -> t2 (completed) -> t1 (active)
	t1 := makeTaskWithParent("t1", "Task 1", "")
	t1.Status = "pending" // active

	t2 := makeTaskWithParent("t2", "Task 2", "t1")
	t2.Status = "completed"

	t3 := makeTaskWithParent("t3", "Task 3", "t2")
	t3.Status = "completed"

	t4 := makeTaskWithParent("t4", "Task 4", "t3")
	t4.Status = "completed"

	t5 := makeTaskWithParent("t5", "Task 5", "t4")
	t5.Status = "pending"

	activeTaskMap := map[string]types.ResolvedTask{
		"t1": t1,
		"t5": t5,
	}
	allTaskMap := map[string]types.ResolvedTask{
		"t1": t1,
		"t2": t2,
		"t3": t3,
		"t4": t4,
		"t5": t5,
	}

	// Starting from t5's parent (t4), should walk all the way to t1
	result := findActiveAncestor("t4", activeTaskMap, allTaskMap)
	if result != "t1" {
		t.Errorf("expected 't1', got '%s'", result)
	}
}
