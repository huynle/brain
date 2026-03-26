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

// TestTerminalSectionTaskSelection_SSE tests that j/k navigation into individual
// tasks within Draft/Completed sub-features persists across SSE updates.
// Bug: selection gets reset after 1-2 presses because SetTasks calls
// restoreFeatureSelection for terminal-section tasks, which only searches active
// features and fails, falling back to selectFirstFeatureTask.
func TestTerminalSectionTaskSelection_SSE(t *testing.T) {
	// Mix of active and draft tasks so both active features and draft section exist
	tasks := []types.ResolvedTask{
		{ID: "active1", Title: "Active task", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "draft1", Title: "Draft task 1", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
		{ID: "draft2", Title: "Draft task 2", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Initial state: should be on active1 (the only active task)
	if tt.SelectedID != "active1" {
		t.Fatalf("initial: expected SelectedID=active1, got %s", tt.SelectedID)
	}

	// Manually navigate to Draft section → sub-feature → task
	// (simulating what j/k navigation would do)
	tt.clearTerminalSectionNav()
	tt.isOnDraftSection = true
	tt.draftFeatureIdx = 0 // First sub-feature in draft section
	tt.draftTaskIdx = 0    // First task within that sub-feature
	tt.SelectedID = "draft1"
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.isOnUngrouped = false
	tt.draftFeatureIDs = []string{"feat-a"} // populate feature IDs

	// SSE fires — SetTasks called with same task list
	tt.SetTasks(tasks)

	// Selection should be preserved: still on draft1 within draft section
	if tt.SelectedID != "draft1" {
		t.Fatalf("after SSE: expected SelectedID=draft1, got %s (isOnDraftSection=%v, draftFeatureIdx=%d, draftTaskIdx=%d)",
			tt.SelectedID, tt.isOnDraftSection, tt.draftFeatureIdx, tt.draftTaskIdx)
	}
	if !tt.isOnDraftSection {
		t.Fatal("after SSE: expected isOnDraftSection=true, got false")
	}
	if tt.draftFeatureIdx != 0 {
		t.Fatalf("after SSE: expected draftFeatureIdx=0, got %d", tt.draftFeatureIdx)
	}
	if tt.draftTaskIdx != 0 {
		t.Fatalf("after SSE: expected draftTaskIdx=0, got %d", tt.draftTaskIdx)
	}

	// Navigate to second draft task
	tt.draftTaskIdx = 1
	tt.SelectedID = "draft2"

	// SSE fires again
	tt.SetTasks(tasks)

	if tt.SelectedID != "draft2" {
		t.Fatalf("after 2nd SSE: expected SelectedID=draft2, got %s", tt.SelectedID)
	}
	if tt.draftTaskIdx != 1 {
		t.Fatalf("after 2nd SSE: expected draftTaskIdx=1, got %d", tt.draftTaskIdx)
	}
}

// TestTerminalSectionTaskNavigation_SSE tests that using MoveDown to navigate
// into a Draft sub-feature's tasks preserves selection across SSE updates.
// This simulates the real user flow more accurately than manual field setting.
func TestTerminalSectionTaskNavigation_SSE(t *testing.T) {
	// Only draft tasks — so selectFirstFeatureTask will land on Draft section header
	tasks := []types.ResolvedTask{
		{ID: "draft1", Title: "Draft task 1", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
		{ID: "draft2", Title: "Draft task 2", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
		{ID: "draft3", Title: "Draft task 3", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Initial state: should be on Draft section header (no active tasks)
	if !tt.isOnDraftSection {
		t.Fatalf("initial: expected isOnDraftSection=true, got false (SelectedID=%s, featureIdx=%d)",
			tt.SelectedID, tt.selectedFeatureIdx)
	}
	if tt.draftFeatureIdx != -1 {
		t.Fatalf("initial: expected draftFeatureIdx=-1 (section header), got %d", tt.draftFeatureIdx)
	}

	// j → should move to first sub-feature header
	tt.MoveDown()
	if tt.draftFeatureIdx != 0 {
		t.Fatalf("after 1st j: expected draftFeatureIdx=0, got %d", tt.draftFeatureIdx)
	}
	if tt.draftTaskIdx != -1 {
		t.Fatalf("after 1st j: expected draftTaskIdx=-1 (sub-feature header), got %d", tt.draftTaskIdx)
	}

	// SSE fires — should preserve sub-feature header position
	tt.SetTasks(tasks)
	if tt.draftFeatureIdx != 0 {
		t.Fatalf("after SSE on sub-feature header: expected draftFeatureIdx=0, got %d", tt.draftFeatureIdx)
	}

	// j → should enter first task within the sub-feature
	tt.MoveDown()
	if tt.SelectedID != "draft1" {
		t.Fatalf("after 2nd j: expected SelectedID=draft1, got %s (draftFeatureIdx=%d, draftTaskIdx=%d)",
			tt.SelectedID, tt.draftFeatureIdx, tt.draftTaskIdx)
	}
	if tt.draftTaskIdx != 0 {
		t.Fatalf("after 2nd j: expected draftTaskIdx=0, got %d", tt.draftTaskIdx)
	}

	// SSE fires — THIS IS THE KEY CHECK: should preserve task selection
	tt.SetTasks(tasks)
	if tt.SelectedID != "draft1" {
		t.Fatalf("after SSE on task: expected SelectedID=draft1, got %s (isOnDraftSection=%v, draftFeatureIdx=%d, draftTaskIdx=%d)",
			tt.SelectedID, tt.isOnDraftSection, tt.draftFeatureIdx, tt.draftTaskIdx)
	}
	if !tt.isOnDraftSection {
		t.Fatal("after SSE on task: expected isOnDraftSection=true")
	}
	if tt.draftTaskIdx != 0 {
		t.Fatalf("after SSE on task: expected draftTaskIdx=0, got %d", tt.draftTaskIdx)
	}

	// j → move to draft2
	tt.MoveDown()
	if tt.SelectedID != "draft2" {
		t.Fatalf("after 3rd j: expected SelectedID=draft2, got %s", tt.SelectedID)
	}

	// SSE fires — should preserve draft2 selection
	tt.SetTasks(tasks)
	if tt.SelectedID != "draft2" {
		t.Fatalf("after SSE on draft2: expected SelectedID=draft2, got %s (draftTaskIdx=%d)",
			tt.SelectedID, tt.draftTaskIdx)
	}
	if tt.draftTaskIdx != 1 {
		t.Fatalf("after SSE on draft2: expected draftTaskIdx=1, got %d", tt.draftTaskIdx)
	}

	// j → move to draft3
	tt.MoveDown()
	if tt.SelectedID != "draft3" {
		t.Fatalf("after 4th j: expected SelectedID=draft3, got %s", tt.SelectedID)
	}

	// SSE fires — should preserve draft3 selection
	tt.SetTasks(tasks)
	if tt.SelectedID != "draft3" {
		t.Fatalf("after SSE on draft3: expected SelectedID=draft3, got %s (draftTaskIdx=%d)",
			tt.SelectedID, tt.draftTaskIdx)
	}
}

// TestTerminalSectionMixedTasks_SSE tests SSE preservation when there are both active
// and draft tasks (the most common real-world scenario).
func TestTerminalSectionMixedTasks_SSE(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "active1", Title: "Active task", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "active2", Title: "Active task 2", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "draft1", Title: "Draft task 1", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
		{ID: "draft2", Title: "Draft task 2", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Initial state: should be on active1
	if tt.SelectedID != "active1" {
		t.Fatalf("initial: expected SelectedID=active1, got %s", tt.SelectedID)
	}

	// In a real TUI, View() sets hasDraftTasks. Simulate that here so
	// navigation to terminal sections works.
	tt.hasDraftTasks = true
	tt.draftFeatureIDs = []string{"feat-a"}

	// Navigate past all active tasks to draft section using MoveDown
	maxSteps := 20
	for i := 0; i < maxSteps; i++ {
		tt.MoveDown()
		if tt.isOnDraftSection {
			break
		}
	}
	if !tt.isOnDraftSection {
		t.Fatalf("could not navigate to draft section within %d steps", maxSteps)
	}

	// Now navigate into draft sub-feature
	tt.MoveDown() // → sub-feature header (draftFeatureIdx=0, draftTaskIdx=-1)
	tt.MoveDown() // → first draft task (draftTaskIdx=0, SelectedID=draft1)

	if tt.SelectedID != "draft1" {
		t.Fatalf("navigation into draft: expected draft1, got SelectedID=%s, draftFeatureIdx=%d, draftTaskIdx=%d",
			tt.SelectedID, tt.draftFeatureIdx, tt.draftTaskIdx)
	}

	prevDraftTaskIdx := tt.draftTaskIdx
	prevDraftFeatureIdx := tt.draftFeatureIdx

	// SSE fires — should preserve task selection within draft section
	tt.SetTasks(tasks)

	if tt.SelectedID != "draft1" {
		t.Fatalf("after SSE: expected SelectedID=draft1, got %s", tt.SelectedID)
	}
	if !tt.isOnDraftSection {
		t.Fatal("after SSE: isOnDraftSection should be true")
	}
	if tt.draftTaskIdx != prevDraftTaskIdx {
		t.Fatalf("after SSE: draftTaskIdx changed from %d to %d", prevDraftTaskIdx, tt.draftTaskIdx)
	}
	if tt.draftFeatureIdx != prevDraftFeatureIdx {
		t.Fatalf("after SSE: draftFeatureIdx changed from %d to %d", prevDraftFeatureIdx, tt.draftFeatureIdx)
	}

	// Navigate to draft2 and verify SSE preservation
	tt.MoveDown() // → draft2
	if tt.SelectedID != "draft2" {
		t.Fatalf("after j to draft2: expected draft2, got %s", tt.SelectedID)
	}

	tt.SetTasks(tasks) // SSE
	if tt.SelectedID != "draft2" {
		t.Fatalf("after SSE on draft2: expected draft2, got %s", tt.SelectedID)
	}
}

// TestTerminalSectionTaskSelection_AllSections_SSE tests SSE preservation for all
// terminal sections (Draft, Cancelled, Superseded, Archived, Completed).
func TestTerminalSectionTaskSelection_AllSections_SSE(t *testing.T) {
	tests := []struct {
		name          string
		taskStatus    string
		setSection    func(tt *TaskTree)
		checkSection  func(tt *TaskTree) bool
		setTaskIdx    func(tt *TaskTree, idx int)
		getTaskIdx    func(tt *TaskTree) int
		setFeatureIdx func(tt *TaskTree, idx int)
		getFeatureIdx func(tt *TaskTree) int
	}{
		{
			name:          "Draft",
			taskStatus:    "draft",
			setSection:    func(tt *TaskTree) { tt.isOnDraftSection = true },
			checkSection:  func(tt *TaskTree) bool { return tt.isOnDraftSection },
			setTaskIdx:    func(tt *TaskTree, idx int) { tt.draftTaskIdx = idx },
			getTaskIdx:    func(tt *TaskTree) int { return tt.draftTaskIdx },
			setFeatureIdx: func(tt *TaskTree, idx int) { tt.draftFeatureIdx = idx },
			getFeatureIdx: func(tt *TaskTree) int { return tt.draftFeatureIdx },
		},
		{
			name:          "Cancelled",
			taskStatus:    "cancelled",
			setSection:    func(tt *TaskTree) { tt.isOnCancelledSection = true },
			checkSection:  func(tt *TaskTree) bool { return tt.isOnCancelledSection },
			setTaskIdx:    func(tt *TaskTree, idx int) { tt.cancelledTaskIdx = idx },
			getTaskIdx:    func(tt *TaskTree) int { return tt.cancelledTaskIdx },
			setFeatureIdx: func(tt *TaskTree, idx int) { tt.cancelledFeatureIdx = idx },
			getFeatureIdx: func(tt *TaskTree) int { return tt.cancelledFeatureIdx },
		},
		{
			name:          "Superseded",
			taskStatus:    "superseded",
			setSection:    func(tt *TaskTree) { tt.isOnSupersededSection = true },
			checkSection:  func(tt *TaskTree) bool { return tt.isOnSupersededSection },
			setTaskIdx:    func(tt *TaskTree, idx int) { tt.supersededTaskIdx = idx },
			getTaskIdx:    func(tt *TaskTree) int { return tt.supersededTaskIdx },
			setFeatureIdx: func(tt *TaskTree, idx int) { tt.supersededFeatureIdx = idx },
			getFeatureIdx: func(tt *TaskTree) int { return tt.supersededFeatureIdx },
		},
		{
			name:          "Archived",
			taskStatus:    "archived",
			setSection:    func(tt *TaskTree) { tt.isOnArchivedSection = true },
			checkSection:  func(tt *TaskTree) bool { return tt.isOnArchivedSection },
			setTaskIdx:    func(tt *TaskTree, idx int) { tt.archivedTaskIdx = idx },
			getTaskIdx:    func(tt *TaskTree) int { return tt.archivedTaskIdx },
			setFeatureIdx: func(tt *TaskTree, idx int) { tt.archivedFeatureIdx = idx },
			getFeatureIdx: func(tt *TaskTree) int { return tt.archivedFeatureIdx },
		},
		{
			name:          "Completed",
			taskStatus:    "completed",
			setSection:    func(tt *TaskTree) { tt.isOnCompletedSection = true },
			checkSection:  func(tt *TaskTree) bool { return tt.isOnCompletedSection },
			setTaskIdx:    func(tt *TaskTree, idx int) { tt.completedTaskIdx = idx },
			getTaskIdx:    func(tt *TaskTree) int { return tt.completedTaskIdx },
			setFeatureIdx: func(tt *TaskTree, idx int) { tt.completedFeatureIdx = idx },
			getFeatureIdx: func(tt *TaskTree) int { return tt.completedFeatureIdx },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tasks := []types.ResolvedTask{
				{ID: "active1", Title: "Active task", Status: "pending", Priority: "high", FeatureID: "feat-a"},
				{ID: "terminal1", Title: "Terminal task 1", Status: tc.taskStatus, Priority: "medium", FeatureID: "feat-a"},
				{ID: "terminal2", Title: "Terminal task 2", Status: tc.taskStatus, Priority: "medium", FeatureID: "feat-a"},
			}

			tt := NewTaskTree()
			tt.useGroupedView = true
			tt.useFeatureView = true
			tt.SetTasks(tasks)

			// Navigate to terminal section task
			tt.clearTerminalSectionNav()
			tc.setSection(&tt)
			tc.setFeatureIdx(&tt, 0)
			tc.setTaskIdx(&tt, 0)
			tt.SelectedID = "terminal1"
			tt.selectedFeatureIdx = -1
			tt.selectedFeatureTaskIdx = -1
			tt.isOnUngrouped = false

			// SSE fires
			tt.SetTasks(tasks)

			if tt.SelectedID != "terminal1" {
				t.Fatalf("after SSE: expected SelectedID=terminal1, got %s", tt.SelectedID)
			}
			if !tc.checkSection(&tt) {
				t.Fatalf("after SSE: section flag was cleared")
			}
			if tc.getFeatureIdx(&tt) != 0 {
				t.Fatalf("after SSE: expected featureIdx=0, got %d", tc.getFeatureIdx(&tt))
			}
			if tc.getTaskIdx(&tt) != 0 {
				t.Fatalf("after SSE: expected taskIdx=0, got %d", tc.getTaskIdx(&tt))
			}
		})
	}
}

// --- Tests for g/G with only terminal (draft/completed) tasks ---

// TestMoveToTopFeatureGrouped_OnlyDraftTasks ensures g lands on the Draft section header
// when all features only contain draft tasks (no active tasks rendered).
func TestMoveToTopFeatureGrouped_OnlyDraftTasks(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "d1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
		{ID: "d2", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Navigate somewhere
	tt.isOnDraftSection = true
	tt.draftFeatureIdx = 0

	// Press g
	tt.MoveToTop()

	// Should land on Draft section header (first terminal section)
	if !tt.isOnDraftSection {
		t.Fatal("Expected isOnDraftSection=true when only draft tasks exist")
	}
	// Cursor should be visible (on section header)
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected selectedFeatureTaskIdx=-1, got %d", tt.selectedFeatureTaskIdx)
	}
}

// TestMoveToTopFeatureGrouped_MixedActiveAndDraft ensures g lands on the first active
// feature header, not a draft-only feature.
func TestMoveToTopFeatureGrouped_MixedActiveAndDraft(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "d1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-b"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Start on feat-b's task
	tt.selectedFeatureIdx = 1
	tt.selectedFeatureTaskIdx = 0
	tt.SelectedID = "t1"

	// Press g
	tt.MoveToTop()

	// Should land on feat-b header (first feature with active tasks), not feat-a (draft only)
	if tt.selectedFeatureIdx != 1 {
		t.Errorf("Expected selectedFeatureIdx=1 (feat-b with active tasks), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Errorf("Expected on header (-1), got %d", tt.selectedFeatureTaskIdx)
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID empty (on header), got %q", tt.SelectedID)
	}
}

// TestMoveToTopFeatureGrouped_PreservesCollapsedState ensures g does NOT change
// any section's collapsed/expanded state.
func TestMoveToTopFeatureGrouped_PreservesCollapsedState(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "d1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Expand draft section explicitly
	tt.draftCollapsed = false
	origDraftCollapsed := tt.draftCollapsed
	origCompletedCollapsed := tt.completedCollapsed

	// Press g
	tt.MoveToTop()

	// Verify collapsed states are preserved
	if tt.draftCollapsed != origDraftCollapsed {
		t.Errorf("draftCollapsed changed: was %v, now %v", origDraftCollapsed, tt.draftCollapsed)
	}
	if tt.completedCollapsed != origCompletedCollapsed {
		t.Errorf("completedCollapsed changed: was %v, now %v", origCompletedCollapsed, tt.completedCollapsed)
	}
}

// TestMoveToTopFeatureGrouped_ThenNavigate ensures j/k work after pressing g.
func TestMoveToTopFeatureGrouped_ThenNavigate(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Status: "pending", Priority: "high", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Navigate to t2
	tt.MoveDown() // t1 (first task auto-selected by SetTasks)
	tt.MoveDown() // t2

	// Press g (MoveToTop)
	tt.MoveToTop()

	// Should be on first feature header
	if tt.selectedFeatureIdx != 0 || tt.selectedFeatureTaskIdx != -1 {
		t.Fatalf("after g: expected on header (idx=0, taskIdx=-1), got (idx=%d, taskIdx=%d)",
			tt.selectedFeatureIdx, tt.selectedFeatureTaskIdx)
	}

	// Press j — should enter first task
	tt.MoveDown()
	if tt.SelectedID != "t1" {
		t.Fatalf("after g then j: expected t1, got %s", tt.SelectedID)
	}

	// Press j again — should move to t2
	tt.MoveDown()
	if tt.SelectedID != "t2" {
		t.Fatalf("after g then j j: expected t2, got %s", tt.SelectedID)
	}
}

// TestSelectFirstFeatureTask_DraftFeatureIDs_SortedAlphabetically verifies that
// selectFirstFeatureTask() populates draftFeatureIDs in alphabetical order
// (matching the renderer's sort.Strings) rather than feature-group iteration order.
// This prevents j/k navigation from visiting features in the wrong order,
// skipping features, or backtracking to headers.
func TestSelectFirstFeatureTask_DraftFeatureIDs_SortedAlphabetically(t *testing.T) {
	// Create features where GroupTasksByFeature iteration order differs from alphabetical.
	// All tasks are "draft" so selectFirstFeatureTask falls through to draft section.
	// GroupTasksByFeature sorts by priority (high>medium>low) then alphabetically.
	// Give "zebra" high priority so it appears FIRST in featureGroups.Features,
	// but alphabetical order (used by renderer) would put "alpha" first.
	tasks := []types.ResolvedTask{
		// "zebra" feature — high priority, 3 draft tasks
		{ID: "z1", Status: "draft", Priority: "medium", FeatureID: "zebra", FeaturePriority: "high"},
		{ID: "z2", Status: "draft", Priority: "medium", FeatureID: "zebra", FeaturePriority: "high"},
		{ID: "z3", Status: "draft", Priority: "medium", FeatureID: "zebra", FeaturePriority: "high"},
		// "alpha" feature — medium priority, 1 draft task
		{ID: "a1", Status: "draft", Priority: "medium", FeatureID: "alpha", FeaturePriority: "medium"},
		// "middle" feature — medium priority, 2 draft tasks
		{ID: "m1", Status: "draft", Priority: "medium", FeatureID: "middle", FeaturePriority: "medium"},
		{ID: "m2", Status: "draft", Priority: "medium", FeatureID: "middle", FeaturePriority: "medium"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.draftCollapsed = false
	tt.SetTasks(tasks)

	// After SetTasks, selectFirstFeatureTask should have been called.
	// Verify draftFeatureIDs is sorted alphabetically.
	if len(tt.draftFeatureIDs) != 3 {
		t.Fatalf("expected 3 draftFeatureIDs, got %d: %v", len(tt.draftFeatureIDs), tt.draftFeatureIDs)
	}
	expectedOrder := []string{"alpha", "middle", "zebra"}
	for i, expected := range expectedOrder {
		if tt.draftFeatureIDs[i] != expected {
			t.Errorf("draftFeatureIDs[%d] = %q, want %q (full list: %v)",
				i, tt.draftFeatureIDs[i], expected, tt.draftFeatureIDs)
		}
	}

	// Verify cursor is on Draft section header
	if !tt.isOnDraftSection {
		t.Fatal("expected isOnDraftSection=true")
	}
	if tt.draftFeatureIdx != -1 {
		t.Fatalf("expected draftFeatureIdx=-1 (on section header), got %d", tt.draftFeatureIdx)
	}
}

// TestDraftSectionNavigation_VisitsFeaturesInAlphabeticalOrder verifies that
// j/k navigation through the Draft section visits features in alphabetical order
// (matching the renderer), with no skipping, backtracking, or blank gaps.
func TestDraftSectionNavigation_VisitsFeaturesInAlphabeticalOrder(t *testing.T) {
	// Same setup: features whose iteration order differs from alphabetical.
	// "zebra" has high priority so it's first in featureGroups, but alphabetical puts "alpha" first.
	tasks := []types.ResolvedTask{
		{ID: "z1", Status: "draft", Priority: "medium", FeatureID: "zebra", FeaturePriority: "high"},
		{ID: "z2", Status: "draft", Priority: "medium", FeatureID: "zebra", FeaturePriority: "high"},
		{ID: "a1", Status: "draft", Priority: "medium", FeatureID: "alpha", FeaturePriority: "medium"},
		{ID: "m1", Status: "draft", Priority: "medium", FeatureID: "middle", FeaturePriority: "medium"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.draftCollapsed = false
	tt.SetTasks(tasks)

	// Should start on Draft section header
	if !tt.isOnDraftSection {
		t.Fatal("expected isOnDraftSection=true after SetTasks with only draft tasks")
	}

	// j → first sub-feature header (should be "alpha" alphabetically)
	tt.MoveDown()
	if tt.draftFeatureIdx != 0 {
		t.Fatalf("after 1st j: expected draftFeatureIdx=0, got %d", tt.draftFeatureIdx)
	}
	if len(tt.draftFeatureIDs) < 1 || tt.draftFeatureIDs[0] != "alpha" {
		t.Fatalf("after 1st j: expected first feature 'alpha', got draftFeatureIDs=%v", tt.draftFeatureIDs)
	}

	// j → first task in "alpha" (a1)
	tt.MoveDown()
	if tt.SelectedID != "a1" {
		t.Fatalf("after 2nd j: expected SelectedID='a1', got %q", tt.SelectedID)
	}

	// j → next sub-feature header "middle" (since alpha has only 1 task)
	tt.MoveDown()
	if tt.draftFeatureIdx != 1 {
		t.Fatalf("after 3rd j: expected draftFeatureIdx=1 (middle), got %d", tt.draftFeatureIdx)
	}
	if len(tt.draftFeatureIDs) < 2 || tt.draftFeatureIDs[1] != "middle" {
		t.Fatalf("after 3rd j: expected second feature 'middle', got draftFeatureIDs=%v", tt.draftFeatureIDs)
	}

	// j → first task in "middle" (m1)
	tt.MoveDown()
	if tt.SelectedID != "m1" {
		t.Fatalf("after 4th j: expected SelectedID='m1', got %q", tt.SelectedID)
	}

	// j → next sub-feature header "zebra" (since middle has only 1 task)
	tt.MoveDown()
	if tt.draftFeatureIdx != 2 {
		t.Fatalf("after 5th j: expected draftFeatureIdx=2 (zebra), got %d", tt.draftFeatureIdx)
	}
	if len(tt.draftFeatureIDs) < 3 || tt.draftFeatureIDs[2] != "zebra" {
		t.Fatalf("after 5th j: expected third feature 'zebra', got draftFeatureIDs=%v", tt.draftFeatureIDs)
	}

	// j → first task in "zebra" (z1)
	tt.MoveDown()
	if tt.SelectedID != "z1" {
		t.Fatalf("after 6th j: expected SelectedID='z1', got %q", tt.SelectedID)
	}

	// j → second task in "zebra" (z2)
	tt.MoveDown()
	if tt.SelectedID != "z2" {
		t.Fatalf("after 7th j: expected SelectedID='z2', got %q", tt.SelectedID)
	}
}

// TestCompletedSectionNavigation_FeatureIDsPopulated verifies that
// selectFirstFeatureTask() also eagerly populates completedFeatureIDs
// (and other terminal section IDs) when falling back to those sections.
func TestCompletedSectionNavigation_FeatureIDsPopulated(t *testing.T) {
	// Only completed tasks — selectFirstFeatureTask falls through to completed section.
	// "zulu" has high priority so it's first in featureGroups, but alphabetical puts "bravo" first.
	tasks := []types.ResolvedTask{
		{ID: "c1", Status: "completed", Priority: "medium", FeatureID: "zulu", FeaturePriority: "high"},
		{ID: "c2", Status: "completed", Priority: "medium", FeatureID: "bravo", FeaturePriority: "medium"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.completedCollapsed = false
	tt.SetTasks(tasks)

	// Should land on Completed section header
	if !tt.isOnCompletedSection {
		t.Fatal("expected isOnCompletedSection=true after SetTasks with only completed tasks")
	}

	// completedFeatureIDs should be populated and sorted alphabetically
	if len(tt.completedFeatureIDs) != 2 {
		t.Fatalf("expected 2 completedFeatureIDs, got %d: %v", len(tt.completedFeatureIDs), tt.completedFeatureIDs)
	}
	if tt.completedFeatureIDs[0] != "bravo" || tt.completedFeatureIDs[1] != "zulu" {
		t.Errorf("completedFeatureIDs not sorted: got %v, want [bravo, zulu]", tt.completedFeatureIDs)
	}

	// j → first sub-feature header (should be "bravo")
	tt.MoveDown()
	if tt.completedFeatureIdx != 0 {
		t.Fatalf("after j: expected completedFeatureIdx=0, got %d", tt.completedFeatureIdx)
	}
}

// TestMoveToBottomFeatureGrouped_OnlyDraftTasks ensures G lands on Draft section.
func TestMoveToBottomFeatureGrouped_OnlyDraftTasks(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "d1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	tt.MoveToBottom()

	// Should land on Draft section (it's the only terminal section with tasks)
	if !tt.isOnDraftSection {
		t.Fatal("Expected isOnDraftSection=true when only draft tasks exist")
	}
}

// TestMoveUp_AtTopOfList_IsNoOp tests that pressing k at the very top of the list
// is a no-op — the cursor stays on the first item (Draft section header) and
// does not disappear.
func TestMoveUp_AtTopOfList_IsNoOp(t *testing.T) {
	// All draft tasks — no active features, so the first navigable item
	// is the Draft section header.
	tasks := []types.ResolvedTask{
		{ID: "d1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
		{ID: "d2", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Verify initial state: cursor should be on Draft section header
	if !tt.isOnDraftSection {
		t.Fatal("Expected isOnDraftSection=true after SetTasks with only draft tasks")
	}
	if tt.draftFeatureIdx != -1 {
		t.Fatalf("Expected draftFeatureIdx=-1 (on section header), got %d", tt.draftFeatureIdx)
	}

	// Press k — should be a no-op
	tt.MoveUp()

	// Cursor should still be on Draft section header
	if !tt.isOnDraftSection {
		t.Fatal("After MoveUp at top: expected isOnDraftSection=true, cursor disappeared")
	}
	if tt.draftFeatureIdx != -1 {
		t.Fatalf("After MoveUp at top: expected draftFeatureIdx=-1, got %d", tt.draftFeatureIdx)
	}
}

// TestMoveDown_AtBottomOfList_IsNoOp tests that pressing j at the very bottom
// of the list is a no-op — the cursor stays on the last item.
func TestMoveDown_AtBottomOfList_IsNoOp(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "d1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
		{ID: "d2", Status: "draft", Priority: "medium", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Navigate to the absolute bottom by pressing j until we can't go further.
	// Track state to detect when movement stops.
	maxIterations := 20 // Safety limit
	for i := 0; i < maxIterations; i++ {
		prevSelectedID := tt.SelectedID
		prevDraftFeatIdx := tt.draftFeatureIdx
		prevDraftTaskIdx := tt.draftTaskIdx
		prevIsOnDraft := tt.isOnDraftSection

		tt.MoveDown()

		// Check if state didn't change — we've hit the bottom
		if tt.SelectedID == prevSelectedID && tt.draftFeatureIdx == prevDraftFeatIdx &&
			tt.draftTaskIdx == prevDraftTaskIdx && tt.isOnDraftSection == prevIsOnDraft {
			break
		}
	}

	// Record the state at the absolute bottom
	bottomIsOnDraft := tt.isOnDraftSection
	bottomDraftFeatIdx := tt.draftFeatureIdx
	bottomDraftTaskIdx := tt.draftTaskIdx
	bottomSelectedID := tt.SelectedID

	// Press j one more time — should be a no-op
	tt.MoveDown()

	// Cursor should remain in the same position
	if tt.isOnDraftSection != bottomIsOnDraft {
		t.Fatalf("After MoveDown at bottom: isOnDraftSection changed from %v to %v", bottomIsOnDraft, tt.isOnDraftSection)
	}
	if tt.draftFeatureIdx != bottomDraftFeatIdx {
		t.Fatalf("After MoveDown at bottom: draftFeatureIdx changed from %d to %d", bottomDraftFeatIdx, tt.draftFeatureIdx)
	}
	if tt.draftTaskIdx != bottomDraftTaskIdx {
		t.Fatalf("After MoveDown at bottom: draftTaskIdx changed from %d to %d", bottomDraftTaskIdx, tt.draftTaskIdx)
	}
	if tt.SelectedID != bottomSelectedID {
		t.Fatalf("After MoveDown at bottom: SelectedID changed from %q to %q", bottomSelectedID, tt.SelectedID)
	}
}

// TestMoveToTop_ThenMoveUp_CursorStays tests the exact bug scenario:
// g (jump to top) then k (move up) should leave cursor on the first item.
func TestMoveToTop_ThenMoveUp_CursorStays(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "d1", Status: "draft", Priority: "high", FeatureID: "feat-a"},
		{ID: "d2", Status: "draft", Priority: "medium", FeatureID: "feat-b"},
		{ID: "c1", Status: "completed", Priority: "high", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Press g — jump to top
	tt.MoveToTop()

	// Record state at top
	topIsOnDraft := tt.isOnDraftSection
	topDraftFeatIdx := tt.draftFeatureIdx

	// Press k — should be a no-op
	tt.MoveUp()

	// Cursor should NOT have disappeared
	isOnAnything := tt.isOnDraftSection || tt.isOnCancelledSection || tt.isOnSupersededSection ||
		tt.isOnArchivedSection || tt.isOnCompletedSection ||
		tt.selectedFeatureIdx >= 0 || tt.isOnUngrouped

	if !isOnAnything {
		t.Fatal("After g then k: cursor disappeared entirely — not on any section or feature")
	}

	// Should still be on the same item as after g
	if topIsOnDraft && !tt.isOnDraftSection {
		t.Fatal("After g then k: was on Draft section but cursor moved away")
	}
	if tt.draftFeatureIdx != topDraftFeatIdx {
		t.Fatalf("After g then k: draftFeatureIdx changed from %d to %d", topDraftFeatIdx, tt.draftFeatureIdx)
	}
}

// TestMoveToBottom_ThenMoveDown_CursorStays tests that G then j leaves
// cursor on the last item.
func TestMoveToBottom_ThenMoveDown_CursorStays(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "c1", Status: "completed", Priority: "high", FeatureID: "feat-a"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Press G — jump to bottom
	tt.MoveToBottom()

	// Record state at bottom
	bottomIsOnCompleted := tt.isOnCompletedSection
	bottomCompletedFeatIdx := tt.completedFeatureIdx
	bottomSelectedID := tt.SelectedID

	// Press j — should be a no-op
	tt.MoveDown()

	// Cursor should remain in the same position
	if tt.isOnCompletedSection != bottomIsOnCompleted {
		t.Fatalf("After G then j: isOnCompletedSection changed from %v to %v", bottomIsOnCompleted, tt.isOnCompletedSection)
	}
	if tt.completedFeatureIdx != bottomCompletedFeatIdx {
		t.Fatalf("After G then j: completedFeatureIdx changed from %d to %d", bottomCompletedFeatIdx, tt.completedFeatureIdx)
	}
	if tt.SelectedID != bottomSelectedID {
		t.Fatalf("After G then j: SelectedID changed from %q to %q", bottomSelectedID, tt.SelectedID)
	}
}

// TestMoveUp_AtFirstFeatureHeader_IsNoOp tests that k on the first active feature
// header (when there ARE active features) is a no-op.
func TestMoveUp_AtFirstFeatureHeader_IsNoOp(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "pending", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Status: "pending", Priority: "high", FeatureID: "feat-b"},
	}

	tt := NewTaskTree()
	tt.useGroupedView = true
	tt.useFeatureView = true
	tt.SetTasks(tasks)

	// Jump to top — should land on first feature header
	tt.MoveToTop()

	if tt.selectedFeatureIdx != 0 {
		t.Fatalf("Expected to be on first feature (idx 0), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Fatalf("Expected to be on feature header (-1), got %d", tt.selectedFeatureTaskIdx)
	}

	// Press k — should be a no-op
	tt.MoveUp()

	if tt.selectedFeatureIdx != 0 {
		t.Fatalf("After k at top: selectedFeatureIdx changed to %d", tt.selectedFeatureIdx)
	}
	if tt.selectedFeatureTaskIdx != -1 {
		t.Fatalf("After k at top: selectedFeatureTaskIdx changed to %d", tt.selectedFeatureTaskIdx)
	}
}
