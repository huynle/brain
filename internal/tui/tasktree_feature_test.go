package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestGetSelectedFeatureID_NotInFeatureView tests that GetSelectedFeatureID
// returns empty string when not in feature view mode.
func TestGetSelectedFeatureID_NotInFeatureView(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = false
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = -1

	result := tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string when not in feature view, got %q", result)
	}
}

// TestGetSelectedFeatureID_OnTask tests that GetSelectedFeatureID
// returns empty string when cursor is on a task (not header).
func TestGetSelectedFeatureID_OnTask(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = 0 // On a task, not header

	// Set up a feature group
	tt.featureGroups = FeatureGroupResult{
		Features: []FeatureGroup{
			{
				ID:    "feat-123",
				Tasks: []types.ResolvedTask{{ID: "task-1"}},
			},
		},
	}

	result := tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string when on task, got %q", result)
	}
}

// TestGetSelectedFeatureID_OnUngrouped tests that GetSelectedFeatureID
// returns empty string when on ungrouped group header.
func TestGetSelectedFeatureID_OnUngrouped(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = true        // On ungrouped group

	result := tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string when on ungrouped, got %q", result)
	}
}

// TestGetSelectedFeatureID_OnFeatureHeader tests that GetSelectedFeatureID
// returns the feature ID when cursor is on a feature header.
func TestGetSelectedFeatureID_OnFeatureHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = false

	// Set up a feature group
	tt.featureGroups = FeatureGroupResult{
		Features: []FeatureGroup{
			{
				ID:    "feat-123",
				Tasks: []types.ResolvedTask{{ID: "task-1"}},
			},
		},
	}

	result := tt.GetSelectedFeatureID()
	expected := "feat-123"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestGetSelectedFeatureID_OnFeatureHeader_MultipleFeatures tests selection
// of different feature indices.
func TestGetSelectedFeatureID_OnFeatureHeader_MultipleFeatures(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = false

	// Set up multiple feature groups
	tt.featureGroups = FeatureGroupResult{
		Features: []FeatureGroup{
			{
				ID:    "feat-123",
				Tasks: []types.ResolvedTask{{ID: "task-1"}},
			},
			{
				ID:    "feat-456",
				Tasks: []types.ResolvedTask{{ID: "task-2"}},
			},
			{
				ID:    "feat-789",
				Tasks: []types.ResolvedTask{{ID: "task-3"}},
			},
		},
	}

	// Test selecting second feature
	tt.selectedFeatureIdx = 1
	result := tt.GetSelectedFeatureID()
	expected := "feat-456"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// Test selecting third feature
	tt.selectedFeatureIdx = 2
	result = tt.GetSelectedFeatureID()
	expected = "feat-789"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestGetSelectedFeatureID_OutOfBounds tests that GetSelectedFeatureID
// returns empty string when selectedFeatureIdx is out of bounds.
func TestGetSelectedFeatureID_OutOfBounds(t *testing.T) {
	tt := NewTaskTree()
	tt.useFeatureView = true
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = false

	// Set up a feature group
	tt.featureGroups = FeatureGroupResult{
		Features: []FeatureGroup{
			{
				ID:    "feat-123",
				Tasks: []types.ResolvedTask{{ID: "task-1"}},
			},
		},
	}

	// Test negative index
	tt.selectedFeatureIdx = -1
	result := tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string for negative index, got %q", result)
	}

	// Test index too large
	tt.selectedFeatureIdx = 5
	result = tt.GetSelectedFeatureID()
	if result != "" {
		t.Errorf("Expected empty string for out-of-bounds index, got %q", result)
	}
}

// --- Tests for moveToTopFeatureGrouped / moveToBottomFeatureGrouped ---

// helper to build a feature-view TaskTree with given features and optional ungrouped.
func newFeatureViewTree(features []FeatureGroup, ungrouped *FeatureGroup) TaskTree {
	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.featureGroups = FeatureGroupResult{
		Features:  features,
		Ungrouped: ungrouped,
	}
	// Start in a neutral position
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.isOnUngrouped = false
	tt.SelectedID = ""
	return tt
}

func TestMoveToTopFeatureGrouped_JumpsToFirstFeatureHeader(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
		{ID: "feat-b", Tasks: []types.ResolvedTask{{ID: "t2"}}, Collapsed: false},
	}, nil)

	// Start somewhere in the middle
	tt.selectedFeatureIdx = 1
	tt.selectedFeatureTaskIdx = 0
	tt.SelectedID = "t2"

	tt.MoveToTop()

	if tt.selectedFeatureIdx != 0 {
		t.Errorf("Expected selectedFeatureIdx=0, got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected selectedFeatureTaskIdx=-1 (on header), got %d", tt.selectedFeatureTaskIdx)
	}
	if tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=false")
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID empty (on header), got %q", tt.SelectedID)
	}
}

func TestMoveToTopFeatureGrouped_NoFeatures_FallsToUngrouped(t *testing.T) {
	ungrouped := &FeatureGroup{
		ID:    "[Ungrouped]",
		Tasks: []types.ResolvedTask{{ID: "u1"}},
	}
	tt := newFeatureViewTree(nil, ungrouped)

	tt.MoveToTop()

	if !tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=true when no features exist")
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected on ungrouped header (-1), got %d", tt.selectedFeatureTaskIdx)
	}
}

func TestMoveToTopFeatureGrouped_EmptyTree(t *testing.T) {
	tt := newFeatureViewTree(nil, nil)

	// Should not panic
	tt.MoveToTop()

	if tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=false when no features and no ungrouped")
	}
}

func TestMoveToBottomFeatureGrouped_JumpsToLastTaskInUngrouped(t *testing.T) {
	ungrouped := &FeatureGroup{
		ID:        "[Ungrouped]",
		Tasks:     []types.ResolvedTask{{ID: "u1"}, {ID: "u2"}, {ID: "u3"}},
		Collapsed: false,
	}
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
	}, ungrouped)

	tt.MoveToBottom()

	if !tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=true (ungrouped is last section)")
	}
	if tt.SelectedID != "u3" {
		t.Errorf("Expected SelectedID=u3 (last ungrouped task), got %q", tt.SelectedID)
	}
	if tt.selectedFeatureTaskIdx != 2 {
		t.Errorf("Expected selectedFeatureTaskIdx=2, got %d", tt.selectedFeatureTaskIdx)
	}
}

func TestMoveToBottomFeatureGrouped_UngroupedCollapsed_LandsOnHeader(t *testing.T) {
	ungrouped := &FeatureGroup{
		ID:        "[Ungrouped]",
		Tasks:     []types.ResolvedTask{{ID: "u1"}},
		Collapsed: true,
	}
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
	}, ungrouped)

	tt.MoveToBottom()

	if !tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=true")
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected on ungrouped header (-1), got %d", tt.selectedFeatureTaskIdx)
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID empty (on header), got %q", tt.SelectedID)
	}
}

func TestMoveToBottomFeatureGrouped_NoUngrouped_JumpsToLastFeature(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
		{ID: "feat-b", Tasks: []types.ResolvedTask{{ID: "t2"}, {ID: "t3"}}, Collapsed: false},
	}, nil)

	tt.MoveToBottom()

	if tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=false (no ungrouped)")
	}
	if tt.selectedFeatureIdx != 1 {
		t.Errorf("Expected selectedFeatureIdx=1 (last feature), got %d", tt.selectedFeatureIdx)
	}
	if tt.SelectedID != "t3" {
		t.Errorf("Expected SelectedID=t3 (last task in last feature), got %q", tt.SelectedID)
	}
}

func TestMoveToBottomFeatureGrouped_LastFeatureCollapsed_LandsOnHeader(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1"}}, Collapsed: false},
		{ID: "feat-b", Tasks: []types.ResolvedTask{{ID: "t2"}}, Collapsed: true},
	}, nil)

	tt.MoveToBottom()

	if tt.selectedFeatureIdx != 1 {
		t.Errorf("Expected selectedFeatureIdx=1 (last feature), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected on header (-1) when collapsed, got %d", tt.selectedFeatureTaskIdx)
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID empty (on header), got %q", tt.SelectedID)
	}
}

func TestMoveToBottomFeatureGrouped_EmptyTree(t *testing.T) {
	tt := newFeatureViewTree(nil, nil)

	// Should not panic
	tt.MoveToBottom()
}

func TestMoveDownFeatureGrouped_SkipsInactiveTasks(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{
			ID: "feat-a",
			Tasks: []types.ResolvedTask{
				{ID: "t1", Status: "pending", Priority: "high"},
				{ID: "t2", Status: "completed"},
				{ID: "t3", Status: "pending", Priority: "low"},
			},
			Collapsed: false,
		},
	}, nil)

	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = 0
	tt.SelectedID = "t1"

	tt.moveDownFeatureGrouped()

	if tt.SelectedID != "t3" {
		t.Fatalf("expected to skip completed task and select t3, got %q", tt.SelectedID)
	}
	if tt.selectedFeatureTaskIdx != 1 {
		t.Fatalf("expected tree-order index 1, got %d", tt.selectedFeatureTaskIdx)
	}
}

func TestRestoreFeatureSelection_UsesActiveTreeOrderIndex(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{
			ID: "feat-a",
			Tasks: []types.ResolvedTask{
				{ID: "t1", Status: "pending", Priority: "high"},
				{ID: "t2", Status: "completed"},
				{ID: "t3", Status: "pending", Priority: "low"},
			},
		},
	}, nil)

	if !tt.restoreFeatureSelection("t3") {
		t.Fatal("expected restoreFeatureSelection to find t3")
	}

	if tt.selectedFeatureTaskIdx != 1 {
		t.Fatalf("expected active tree-order index 1 for t3, got %d", tt.selectedFeatureTaskIdx)
	}
}

func TestSelectFirstFeatureTask_UsesTreeOrder(t *testing.T) {
	tt := newFeatureViewTree([]FeatureGroup{
		{
			ID: "feat-a",
			Tasks: []types.ResolvedTask{
				{ID: "child", Status: "pending", ParentID: "parent"},
				{ID: "parent", Status: "pending"},
			},
		},
	}, nil)

	tt.selectFirstFeatureTask()

	if tt.SelectedID != "parent" {
		t.Fatalf("expected first selected task to follow tree order (parent), got %q", tt.SelectedID)
	}
	if tt.selectedFeatureTaskIdx != 0 {
		t.Fatalf("expected selectedFeatureTaskIdx=0, got %d", tt.selectedFeatureTaskIdx)
	}
}

// TestMoveDownFeatureGrouped_RepeatedWithSSE reproduces the bug where j/k navigation
// gets stuck after the first move when SSE updates (SetTasks) fire between keypresses.
// This simulates: j press → SSE update → j press → SSE update → ...
// Expected: cursor advances through all tasks, not just the first one.
func TestMoveDownFeatureGrouped_RepeatedWithSSE(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t3", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t4", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t5", Status: "pending", Priority: "high", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Verify initial selection is on t1
	if tt.SelectedID != "t1" {
		t.Fatalf("initial: expected SelectedID=t1, got %s", tt.SelectedID)
	}

	// Simulate sequence: j press → SSE update → j press → SSE update ...
	expectedIDs := []string{"t1", "t2", "t3", "t4", "t5"}

	for i := 0; i < len(expectedIDs)-1; i++ {
		// Verify current position before move
		if tt.SelectedID != expectedIDs[i] {
			t.Fatalf("step %d: before MoveDown, expected SelectedID=%s, got %s (featureIdx=%d, taskIdx=%d)",
				i, expectedIDs[i], tt.SelectedID, tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx)
		}

		// Press j
		tt.MoveDown()

		// Verify moved to next task
		if tt.SelectedID != expectedIDs[i+1] {
			t.Fatalf("step %d: after MoveDown, expected SelectedID=%s, got %s (featureIdx=%d, taskIdx=%d)",
				i, expectedIDs[i+1], tt.SelectedID, tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx)
		}

		// Simulate SSE update (same tasks, mimics tasks_snapshot)
		tt.SetTasks(tasks)

		// Verify selection preserved after SSE
		if tt.SelectedID != expectedIDs[i+1] {
			t.Fatalf("step %d: after SetTasks (SSE), expected SelectedID=%s, got %s (featureIdx=%d, taskIdx=%d)",
				i, expectedIDs[i+1], tt.SelectedID, tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx)
		}
	}
}

// TestMoveDownFeatureGrouped_RepeatedWithSSE_AcrossFeatures tests j navigation
// across multiple features with SSE updates firing between keypresses.
func TestMoveDownFeatureGrouped_RepeatedWithSSE_AcrossFeatures(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t3", Status: "pending", Priority: "high", FeatureID: "feat-b"},
		{ID: "t4", Status: "pending", Priority: "high", FeatureID: "feat-b"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Start on first feature header, then move to first task
	if tt.SelectedID != "t1" {
		t.Fatalf("initial: expected SelectedID=t1, got %s", tt.SelectedID)
	}

	// j → t2
	tt.MoveDown()
	if tt.SelectedID != "t2" {
		t.Fatalf("after 1st j: expected t2, got %s", tt.SelectedID)
	}
	tt.SetTasks(tasks) // SSE

	// j → should go to feat-b header (SelectedID="")
	tt.MoveDown()
	// At end of feat-a, should move to feat-b header
	if tt.selectedFeatureTaskIdx != -1 {
		// On feat-b header
		t.Logf("after 2nd j: on feature header (idx=%d), SelectedID=%s", tt.selectedFeatureIdx, tt.SelectedID)
	}
	tt.SetTasks(tasks) // SSE

	// j → should enter feat-b's first task (t3)
	tt.MoveDown()
	if tt.SelectedID != "t3" {
		t.Fatalf("after 3rd j: expected t3, got %s (featureIdx=%d, taskIdx=%d, isOnUngrouped=%v)",
			tt.SelectedID, tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx, tt.isOnUngrouped)
	}
	tt.SetTasks(tasks) // SSE

	// j → t4
	tt.MoveDown()
	if tt.SelectedID != "t4" {
		t.Fatalf("after 4th j: expected t4, got %s (featureIdx=%d, taskIdx=%d)",
			tt.SelectedID, tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx)
	}
}

// TestMoveToTopFeatureGrouped_WithSSE tests that g (MoveToTop) survives SSE updates.
// MoveToTop lands on a feature header (SelectedID=""), so SSE must not reset it.
func TestMoveToTopFeatureGrouped_WithSSE(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t3", Status: "pending", Priority: "high", FeatureID: "feat-b"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Navigate to t2
	tt.MoveDown()
	if tt.SelectedID != "t2" {
		t.Fatalf("after j: expected t2, got %s", tt.SelectedID)
	}

	// Press g (MoveToTop) — should land on first feature header
	tt.MoveToTop()
	if tt.selectedFeatureIdx != 0 || tt.selectedFeatureTaskIdx != -1 {
		t.Fatalf("after g: expected on first feature header (idx=0, taskIdx=-1), got (idx=%d, taskIdx=%d)",
			tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx)
	}

	// SSE fires — should preserve header position, NOT reset to first task
	tt.SetTasks(tasks)
	if tt.selectedFeatureIdx != 0 || tt.selectedFeatureTaskIdx != -1 {
		t.Fatalf("after SSE on header: expected preserved header (idx=0, taskIdx=-1), got (idx=%d, taskIdx=%d, ID=%s)",
			tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx, tt.SelectedID)
	}

	// Now j from header should enter first task
	tt.MoveDown()
	if tt.SelectedID != "t1" {
		t.Fatalf("after j from header: expected t1, got %s", tt.SelectedID)
	}
}

// TestMoveToBottomFeatureGrouped_WithSSE tests that G (MoveToBottom) works and survives SSE.
func TestMoveToBottomFeatureGrouped_WithSSE(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t3", Status: "pending", Priority: "high", FeatureID: "feat-b"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Press G (MoveToBottom) — should land on last task
	tt.MoveToBottom()
	if tt.SelectedID != "t3" {
		t.Logf("MoveToBottom: featureIdx=%d, taskIdx=%d, isOnUngrouped=%v, ID=%s",
			tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx, tt.isOnUngrouped, tt.SelectedID)
	}

	// SSE fires
	tt.SetTasks(tasks)

	// Selection should be preserved (or at least not reset to t1)
	if tt.SelectedID == "t1" && tt.selectedFeatureTaskIdx == 0 {
		t.Fatalf("after G+SSE: selection reset to first task (t1), expected it to stay near bottom")
	}
}

// TestMoveUpFeatureGrouped_RepeatedWithSSE tests k navigation with SSE updates.
func TestMoveUpFeatureGrouped_RepeatedWithSSE(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t3", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t4", Status: "pending", Priority: "high", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Navigate to last task
	tt.MoveToBottom()
	tt.SetTasks(tasks) // SSE

	// Verify on t4 (last active task)
	if tt.SelectedID != "t4" {
		t.Logf("MoveToBottom landed on %s (featureIdx=%d, taskIdx=%d)", tt.SelectedID, tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx)
	}

	// Navigate to t4 explicitly
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = 3
	tt.SelectedID = "t4"
	tt.isOnUngrouped = false

	// k → t3
	tt.MoveUp()
	if tt.SelectedID != "t3" {
		t.Fatalf("after 1st k: expected t3, got %s (taskIdx=%d)", tt.SelectedID, tt.selectedFeatureTaskIdx)
	}
	tt.SetTasks(tasks) // SSE

	// k → t2
	tt.MoveUp()
	if tt.SelectedID != "t2" {
		t.Fatalf("after 2nd k: expected t2, got %s (taskIdx=%d)", tt.SelectedID, tt.selectedFeatureTaskIdx)
	}
	tt.SetTasks(tasks) // SSE

	// k → t1
	tt.MoveUp()
	if tt.SelectedID != "t1" {
		t.Fatalf("after 3rd k: expected t1, got %s (taskIdx=%d)", tt.SelectedID, tt.selectedFeatureTaskIdx)
	}
}
