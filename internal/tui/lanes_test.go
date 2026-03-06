package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Helper: make a minimal ResolvedTask for lane tests
// =============================================================================

func makeLaneTask(id, title string, dependsOn []string) types.ResolvedTask {
	return makeTask(id, title, "ready", "medium", dependsOn)
}

// =============================================================================
// TopoSort Tests
// =============================================================================

func TestTopoSort_Empty(t *testing.T) {
	result := TopoSort(nil)
	if len(result) != 0 {
		t.Errorf("expected empty slice for nil input, got %d tasks", len(result))
	}

	result = TopoSort([]types.ResolvedTask{})
	if len(result) != 0 {
		t.Errorf("expected empty slice for empty input, got %d tasks", len(result))
	}
}

func TestTopoSort_Single(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("t1", "Task 1", nil),
	}
	result := TopoSort(tasks)

	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	if result[0].ID != "t1" {
		t.Errorf("expected task 't1', got '%s'", result[0].ID)
	}
}

func TestTopoSort_Linear(t *testing.T) {
	// a -> b -> c (linear dependency chain)
	tasks := []types.ResolvedTask{
		makeLaneTask("c", "Task C", []string{"b"}),
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
	}
	result := TopoSort(tasks)

	if len(result) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(result))
	}

	// Should be ordered: a, b, c
	if result[0].ID != "a" {
		t.Errorf("expected first task 'a', got '%s'", result[0].ID)
	}
	if result[1].ID != "b" {
		t.Errorf("expected second task 'b', got '%s'", result[1].ID)
	}
	if result[2].ID != "c" {
		t.Errorf("expected third task 'c', got '%s'", result[2].ID)
	}
}

func TestTopoSort_Fork(t *testing.T) {
	// a -> b, a -> c (fork from a)
	tasks := []types.ResolvedTask{
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("c", "Task C", []string{"a"}),
	}
	result := TopoSort(tasks)

	if len(result) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(result))
	}

	// 'a' must be first
	if result[0].ID != "a" {
		t.Errorf("expected first task 'a', got '%s'", result[0].ID)
	}

	// 'b' and 'c' can be in any order, but both must appear after 'a'
	foundB := false
	foundC := false
	for i := 1; i < len(result); i++ {
		if result[i].ID == "b" {
			foundB = true
		}
		if result[i].ID == "c" {
			foundC = true
		}
	}
	if !foundB || !foundC {
		t.Errorf("expected 'b' and 'c' after 'a', got: %v", result)
	}
}

func TestTopoSort_Merge(t *testing.T) {
	// a -> c, b -> c (merge into c)
	tasks := []types.ResolvedTask{
		makeLaneTask("c", "Task C", []string{"a", "b"}),
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", nil),
	}
	result := TopoSort(tasks)

	if len(result) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(result))
	}

	// 'c' must be last (depends on both a and b)
	if result[2].ID != "c" {
		t.Errorf("expected last task 'c', got '%s'", result[2].ID)
	}

	// 'a' and 'b' can be in any order, but both must appear before 'c'
	foundA := false
	foundB := false
	for i := 0; i < 2; i++ {
		if result[i].ID == "a" {
			foundA = true
		}
		if result[i].ID == "b" {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Errorf("expected 'a' and 'b' before 'c', got: %v", result)
	}
}

func TestTopoSort_Cycle(t *testing.T) {
	// a -> b -> a (cycle)
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", []string{"b"}),
		makeLaneTask("b", "Task B", []string{"a"}),
	}
	result := TopoSort(tasks)

	if len(result) != 2 {
		t.Fatalf("expected 2 tasks (appended at end), got %d", len(result))
	}

	// Both tasks should be present (order doesn't matter for cycles)
	foundA := false
	foundB := false
	for _, task := range result {
		if task.ID == "a" {
			foundA = true
		}
		if task.ID == "b" {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Errorf("expected both 'a' and 'b' in result, got: %v", result)
	}
}

func TestTopoSort_OutOfTreeDeps(t *testing.T) {
	// Task 'b' depends on 'a', but 'a' is not in the task set
	tasks := []types.ResolvedTask{
		makeLaneTask("b", "Task B", []string{"a"}),
	}
	result := TopoSort(tasks)

	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	if result[0].ID != "b" {
		t.Errorf("expected task 'b', got '%s'", result[0].ID)
	}
	// 'b' should be treated as having no in-tree dependencies
}

func TestTopoSort_Diamond(t *testing.T) {
	// Diamond dependency:
	//     a
	//    / \
	//   b   c
	//    \ /
	//     d
	tasks := []types.ResolvedTask{
		makeLaneTask("d", "Task D", []string{"b", "c"}),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"a"}),
		makeLaneTask("a", "Task A", nil),
	}
	result := TopoSort(tasks)

	if len(result) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(result))
	}

	// 'a' must be first
	if result[0].ID != "a" {
		t.Errorf("expected first task 'a', got '%s'", result[0].ID)
	}

	// 'd' must be last
	if result[3].ID != "d" {
		t.Errorf("expected last task 'd', got '%s'", result[3].ID)
	}

	// 'b' and 'c' must be in the middle (after 'a', before 'd')
	foundB := false
	foundC := false
	for i := 1; i < 3; i++ {
		if result[i].ID == "b" {
			foundB = true
		}
		if result[i].ID == "c" {
			foundC = true
		}
	}
	if !foundB || !foundC {
		t.Errorf("expected 'b' and 'c' between 'a' and 'd', got: %v", result)
	}
}
