package tui

import (
	"fmt"
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

// =============================================================================
// DetectMergePoints Tests
// =============================================================================

func TestDetectMergePoints_Empty(t *testing.T) {
	result := DetectMergePoints(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map for nil input, got %d entries", len(result))
	}

	result = DetectMergePoints([]types.ResolvedTask{})
	if len(result) != 0 {
		t.Errorf("expected empty map for empty input, got %d entries", len(result))
	}
}

func TestDetectMergePoints_NoMerges(t *testing.T) {
	// Linear chain: a -> b -> c (no task has 2+ deps)
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"b"}),
	}
	result := DetectMergePoints(tasks)

	if len(result) != 0 {
		t.Errorf("expected no merge points, got %d: %v", len(result), result)
	}
}

func TestDetectMergePoints_SingleMerge(t *testing.T) {
	// Two branches merge: a -> c, b -> c
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", nil),
		makeLaneTask("c", "Task C", []string{"a", "b"}),
	}
	result := DetectMergePoints(tasks)

	if len(result) != 1 {
		t.Fatalf("expected 1 merge point, got %d: %v", len(result), result)
	}

	if !result["c"] {
		t.Errorf("expected 'c' to be a merge point, got: %v", result)
	}
}

func TestDetectMergePoints_Diamond(t *testing.T) {
	// Diamond: a -> b, a -> c, b -> d, c -> d
	// Only 'd' is a merge point (has 2+ deps)
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"a"}),
		makeLaneTask("d", "Task D", []string{"b", "c"}),
	}
	result := DetectMergePoints(tasks)

	if len(result) != 1 {
		t.Fatalf("expected 1 merge point, got %d: %v", len(result), result)
	}

	if !result["d"] {
		t.Errorf("expected 'd' to be a merge point, got: %v", result)
	}
}

func TestDetectMergePoints_OutOfTreeDeps(t *testing.T) {
	// Task 'c' depends on 'a', 'b', and 'x' (out-of-tree)
	// Should only count 'a' and 'b', so 'c' is still a merge point
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", nil),
		makeLaneTask("c", "Task C", []string{"a", "b", "x"}),
	}
	result := DetectMergePoints(tasks)

	if len(result) != 1 {
		t.Fatalf("expected 1 merge point (ignoring out-of-tree dep), got %d: %v", len(result), result)
	}

	if !result["c"] {
		t.Errorf("expected 'c' to be a merge point, got: %v", result)
	}
}

// =============================================================================
// AssignLanes Tests
// =============================================================================

func TestAssignLanes_Empty(t *testing.T) {
	result := AssignLanes(nil)
	if len(result) != 0 {
		t.Errorf("expected empty slice for nil input, got %d assignments", len(result))
	}

	result = AssignLanes([]types.ResolvedTask{})
	if len(result) != 0 {
		t.Errorf("expected empty slice for empty input, got %d assignments", len(result))
	}
}

func TestAssignLanes_SingleRoot(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
	}
	sorted := TopoSort(tasks)
	result := AssignLanes(sorted)

	if len(result) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(result))
	}

	a := result[0]
	if a.TaskID != "a" {
		t.Errorf("expected task 'a', got '%s'", a.TaskID)
	}
	if a.Lane != 0 {
		t.Errorf("expected lane 0, got %d", a.Lane)
	}
	if a.IsMerge {
		t.Errorf("expected IsMerge=false, got true")
	}
	if len(a.MergeFromLanes) != 0 {
		t.Errorf("expected no merge lanes, got %v", a.MergeFromLanes)
	}
}

func TestAssignLanes_Linear(t *testing.T) {
	// a -> b -> c (linear chain, all should be in lane 0)
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"b"}),
	}
	sorted := TopoSort(tasks)
	result := AssignLanes(sorted)

	if len(result) != 3 {
		t.Fatalf("expected 3 assignments, got %d", len(result))
	}

	// All should be in lane 0
	for i, r := range result {
		if r.Lane != 0 {
			t.Errorf("task %d (%s): expected lane 0, got %d", i, r.TaskID, r.Lane)
		}
	}
}

func TestAssignLanes_Fork(t *testing.T) {
	// a -> b, a -> c (fork: parent lane 0, first child lane 0, second child lane 1)
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"a"}),
	}
	sorted := TopoSort(tasks)
	result := AssignLanes(sorted)

	if len(result) != 3 {
		t.Fatalf("expected 3 assignments, got %d", len(result))
	}

	lanes := make(map[string]int)
	for _, r := range result {
		lanes[r.TaskID] = r.Lane
	}

	// 'a' should be in lane 0
	if lanes["a"] != 0 {
		t.Errorf("expected 'a' in lane 0, got %d", lanes["a"])
	}

	// First child should inherit parent's lane (lane 0)
	// Second child should get a new lane (lane 1)
	// We need to check which child was processed first in topo order
	var firstChild, secondChild string
	for _, r := range result {
		if r.TaskID == "b" || r.TaskID == "c" {
			if firstChild == "" {
				firstChild = r.TaskID
			} else {
				secondChild = r.TaskID
			}
		}
	}

	if lanes[firstChild] != 0 {
		t.Errorf("expected first child '%s' in lane 0, got %d", firstChild, lanes[firstChild])
	}
	if lanes[secondChild] != 1 {
		t.Errorf("expected second child '%s' in lane 1, got %d", secondChild, lanes[secondChild])
	}
}

func TestAssignLanes_Merge(t *testing.T) {
	// a -> c, b -> c (merge: two parents merge into c)
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", nil),
		makeLaneTask("c", "Task C", []string{"a", "b"}),
	}
	sorted := TopoSort(tasks)
	result := AssignLanes(sorted)

	if len(result) != 3 {
		t.Fatalf("expected 3 assignments, got %d", len(result))
	}

	var cAssignment *LaneAssignment
	for i := range result {
		if result[i].TaskID == "c" {
			cAssignment = &result[i]
			break
		}
	}

	if cAssignment == nil {
		t.Fatal("task 'c' not found in results")
	}

	if !cAssignment.IsMerge {
		t.Errorf("expected 'c' to be a merge point")
	}

	// Should take the lowest lane (0) and free the other
	if cAssignment.Lane != 0 {
		t.Errorf("expected 'c' in lane 0, got %d", cAssignment.Lane)
	}

	// Should have 1 merge-from lane (the higher numbered one that was freed)
	if len(cAssignment.MergeFromLanes) != 1 {
		t.Errorf("expected 1 merge-from lane, got %d: %v", len(cAssignment.MergeFromLanes), cAssignment.MergeFromLanes)
	}

	if len(cAssignment.MergeFromLanes) > 0 && cAssignment.MergeFromLanes[0] != 1 {
		t.Errorf("expected merge from lane 1, got %d", cAssignment.MergeFromLanes[0])
	}
}

func TestAssignLanes_Diamond(t *testing.T) {
	// Diamond: a -> b, a -> c, b -> d, c -> d
	// Expected: a=0, b=0 (inherits), c=1 (new), d=0 (merge from [0,1])
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"a"}),
		makeLaneTask("d", "Task D", []string{"b", "c"}),
	}
	sorted := TopoSort(tasks)
	result := AssignLanes(sorted)

	if len(result) != 4 {
		t.Fatalf("expected 4 assignments, got %d", len(result))
	}

	lanes := make(map[string]int)
	merges := make(map[string]bool)
	for _, r := range result {
		lanes[r.TaskID] = r.Lane
		if r.IsMerge {
			merges[r.TaskID] = true
		}
	}

	// 'a' should be lane 0
	if lanes["a"] != 0 {
		t.Errorf("expected 'a' in lane 0, got %d", lanes["a"])
	}

	// 'd' should be a merge point taking lane 0
	if !merges["d"] {
		t.Errorf("expected 'd' to be a merge point")
	}
	if lanes["d"] != 0 {
		t.Errorf("expected 'd' in lane 0, got %d", lanes["d"])
	}
}

func TestAssignLanes_LaneCapping(t *testing.T) {
	// Create 10 independent root tasks (no dependencies)
	// With MaxLanes=8, should cap at lane 7
	tasks := []types.ResolvedTask{}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("t%d", i)
		tasks = append(tasks, makeLaneTask(id, "Task "+id, nil))
	}
	sorted := TopoSort(tasks)
	result := AssignLanes(sorted)

	if len(result) != 10 {
		t.Fatalf("expected 10 assignments, got %d", len(result))
	}

	// All lanes should be <= 7 (MaxLanes - 1)
	for _, r := range result {
		if r.Lane >= MaxLanes {
			t.Errorf("task %s: lane %d exceeds MaxLanes-1 (%d)", r.TaskID, r.Lane, MaxLanes-1)
		}
	}
}

func TestAssignLanes_LaneReuse(t *testing.T) {
	// Create a fork-merge pattern that frees lane 1:
	// a(0) -> b(0), c(1) -> merge d(0)
	// Then add e that should reuse freed lane 1
	// Structure: a -> b -> d -> e
	//            └─> c ──┘
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"a"}),
		makeLaneTask("d", "Task D", []string{"b", "c"}),
		makeLaneTask("e", "Task E", []string{"d"}),
	}
	sorted := TopoSort(tasks)
	result := AssignLanes(sorted)

	if len(result) != 5 {
		t.Fatalf("expected 5 assignments, got %d", len(result))
	}

	lanes := make(map[string]int)
	for _, r := range result {
		lanes[r.TaskID] = r.Lane
	}

	// After merge at 'd', lane 1 should be freed
	// Task 'e' should reuse lane 0 (continuing from 'd')
	// This test just verifies lane reuse happens - exact lane depends on topo order
	// Main check: no task uses lane > 1 (only 2 lanes needed total)
	for taskID, lane := range lanes {
		if lane > 1 {
			t.Errorf("task %s uses lane %d, expected at most 1 lane to be needed after merge", taskID, lane)
		}
	}
}

func TestAssignLanes_IndependentTasks(t *testing.T) {
	// Two completely independent tasks (no deps, no dependents)
	// First gets lane 0, frees it immediately
	// Second should reuse lane 0
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", nil),
	}
	sorted := TopoSort(tasks)
	result := AssignLanes(sorted)

	if len(result) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(result))
	}

	// Both should get lane 0 (reuse)
	if result[0].Lane != 0 {
		t.Errorf("first independent task: expected lane 0, got %d", result[0].Lane)
	}
	if result[1].Lane != 0 {
		t.Errorf("second independent task: expected lane 0 (reused), got %d", result[1].Lane)
	}

	// After the first task, its lane is freed, so it's NOT in second task's active lanes
	// The second task's active lanes should only include its own lane (0)
	secondActiveSet := make(map[int]bool)
	for _, l := range result[1].ActiveLanes {
		secondActiveSet[l] = true
	}

	// The second task's own lane should be active
	if !secondActiveSet[result[1].Lane] {
		t.Errorf("second task's lane %d not in its active lanes: %v", result[1].Lane, result[1].ActiveLanes)
	}
}

// =============================================================================
// GeneratePrefix Tests (Phase 3)
// =============================================================================

func TestGeneratePrefix_SimpleBranch(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
	}
	sorted := TopoSort(tasks)
	assignments := AssignLanes(sorted)

	prefix := GeneratePrefix(assignments[0], 0, assignments, nil)
	if prefix != "├─" {
		t.Errorf("expected '├─', got '%s'", prefix)
	}
}

func TestGeneratePrefix_LastChild(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
	}
	sorted := TopoSort(tasks)
	assignments := AssignLanes(sorted)

	prefix := GeneratePrefix(assignments[0], 0, assignments, nil)
	if prefix != "└─" {
		t.Errorf("expected '└─', got '%s'", prefix)
	}
}

func TestGeneratePrefix_TwoLaneMerge(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", nil),
		makeLaneTask("c", "Task C", []string{"a", "b"}),
	}
	sorted := TopoSort(tasks)
	assignments := AssignLanes(sorted)

	var cAssignment LaneAssignment
	var cIndex int
	for i, a := range assignments {
		if a.TaskID == "c" {
			cAssignment = a
			cIndex = i
			break
		}
	}

	prefix := GeneratePrefix(cAssignment, cIndex, assignments, nil)
	if prefix != "╰─┴─" {
		t.Errorf("expected '╰─┴─', got '%s'", prefix)
	}
}

func TestGeneratePrefix_ThreeLaneMerge(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", nil),
		makeLaneTask("c", "Task C", nil),
		makeLaneTask("d", "Task D", []string{"a", "b", "c"}),
	}
	sorted := TopoSort(tasks)
	assignments := AssignLanes(sorted)

	var dAssignment LaneAssignment
	var dIndex int
	for i, a := range assignments {
		if a.TaskID == "d" {
			dAssignment = a
			dIndex = i
			break
		}
	}

	prefix := GeneratePrefix(dAssignment, dIndex, assignments, nil)
	if prefix != "╰─┴─┴─" {
		t.Errorf("expected '╰─┴─┴─', got '%s'", prefix)
	}
}

func TestGeneratePrefix_ActiveLanes(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"a"}),
		makeLaneTask("d", "Task D", []string{"b"}),
		makeLaneTask("e", "Task E", []string{"c"}),
	}
	sorted := TopoSort(tasks)
	assignments := AssignLanes(sorted)

	var eAssignment LaneAssignment
	var eIndex int
	for i, a := range assignments {
		if a.TaskID == "e" {
			eAssignment = a
			eIndex = i
			break
		}
	}

	prefix := GeneratePrefix(eAssignment, eIndex, assignments, nil)
	if prefix != "│ └─" {
		t.Errorf("expected '│ └─', got '%s'", prefix)
	}
}

func TestGeneratePrefixSegments_ContextAware(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
	}
	sorted := TopoSort(tasks)
	assignments := AssignLanes(sorted)

	context := &LanePrefixSegmentContext{
		UpstreamLanes:   map[int]bool{0: true},
		DownstreamLanes: map[int]bool{},
	}

	segments := GeneratePrefixSegments(assignments[0], 0, assignments, context)
	
	if len(segments) == 0 {
		t.Fatal("expected segments, got none")
	}

	foundUpstream := false
	for _, seg := range segments {
		if seg.Lane == 0 && seg.Kind == KindUpstream {
			foundUpstream = true
			break
		}
	}
	if !foundUpstream {
		t.Errorf("expected to find upstream kind for lane 0, got: %+v", segments)
	}
}

func TestGeneratePrefixSegments_RoleTagging(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
	}
	sorted := TopoSort(tasks)
	assignments := AssignLanes(sorted)

	segments := GeneratePrefixSegments(assignments[0], 0, assignments, nil)
	
	foundBranch := false
	for _, seg := range segments {
		if seg.Role == RoleBranch {
			foundBranch = true
			break
		}
	}
	if !foundBranch {
		t.Errorf("expected to find RoleBranch segment, got: %+v", segments)
	}
}
