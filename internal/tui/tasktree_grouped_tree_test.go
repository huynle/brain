package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestViewGrouped_TreeIndentation verifies that grouped view shows tree indentation
// for tasks with dependencies, matching the legacy tree view behavior.
func TestViewGrouped_TreeIndentation(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)         // Enable grouped view
	tt.SetFeatureViewMode(false) // Use classification-only grouped view for this test

	// Create parent-child dependency structure
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent Task", "ready", "high", nil),
		makeTask("child1", "First Child", "waiting", "medium", []string{"parent"}),
		makeTask("child2", "Second Child", "waiting", "medium", []string{"parent"}),
		makeTask("grandchild", "Grandchild Task", "waiting", "low", []string{"child1"}),
	}
	tt.SetTasks(tasks)

	output := tt.viewGrouped(80, 20, "test-project")

	// Should contain tree connectors (box-drawing characters)
	if !strings.Contains(output, "├─") && !strings.Contains(output, "└─") {
		t.Errorf("Expected tree branch characters (├─ or └─) in grouped view, got: %s", output)
	}

	// Should show hierarchical structure
	if !strings.Contains(output, "Parent Task") {
		t.Errorf("Expected 'Parent Task' in output, got: %s", output)
	}
	if !strings.Contains(output, "First Child") {
		t.Errorf("Expected 'First Child' in output, got: %s", output)
	}
	if !strings.Contains(output, "Grandchild Task") {
		t.Errorf("Expected 'Grandchild Task' in output, got: %s", output)
	}

	// Verify indentation exists (tree connectors should be present for children)
	lines := strings.Split(output, "\n")
	foundTreeConnector := false
	for _, line := range lines {
		if strings.Contains(line, "├─") || strings.Contains(line, "└─") {
			foundTreeConnector = true
			break
		}
	}
	if !foundTreeConnector {
		t.Errorf("Expected at least one line with tree connector (├─ or └─), got: %s", output)
	}
}

// TestViewFeatureGrouped_TreeIndentation verifies that feature-grouped view shows
// tree indentation for tasks with dependencies within each feature group.
func TestViewFeatureGrouped_TreeIndentation(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)        // Enable grouped view
	tt.SetFeatureViewMode(true) // Enable feature grouping

	// Create tasks with feature IDs and dependencies
	tasks := []types.ResolvedTask{
		makeTaskWithFeature("feat1-parent", "Feature 1 Parent", "ready", "high", "feature-1", nil),
		makeTaskWithFeature("feat1-child", "Feature 1 Child", "waiting", "medium", "feature-1", []string{"feat1-parent"}),
		makeTaskWithFeature("feat2-task", "Feature 2 Task", "ready", "high", "feature-2", nil),
	}
	tt.SetTasks(tasks)

	output := tt.viewFeatureGrouped(80, 20, "test-project")

	// Should contain tree connectors for feature-1 tasks
	if !strings.Contains(output, "├─") && !strings.Contains(output, "└─") {
		t.Errorf("Expected tree branch characters (├─ or └─) in feature grouped view, got: %s", output)
	}

	// Verify indentation exists
	lines := strings.Split(output, "\n")
	foundTreeConnector := false
	for _, line := range lines {
		if strings.Contains(line, "├─") || strings.Contains(line, "└─") {
			foundTreeConnector = true
			break
		}
	}
	if !foundTreeConnector {
		t.Errorf("Expected at least one line with tree connector in feature view, got: %s", output)
	}
}

// TestViewFeatureGrouped_RelationHighlighting verifies that selecting a task
// in the feature-grouped view computes ancestor/descendant relation maps and
// passes them to renderGroupedTaskLineWithTree without crashing.
func TestViewFeatureGrouped_RelationHighlighting(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	// Chain: a <- b <- c (c depends on b, b depends on a)
	tasks := []types.ResolvedTask{
		makeTaskWithFeature("a", "Task A", "ready", "high", "feat-1", nil),
		makeTaskWithFeature("b", "Task B", "waiting", "medium", "feat-1", []string{"a"}),
		makeTaskWithFeature("c", "Task C", "waiting", "medium", "feat-1", []string{"b"}),
	}
	tt.SetTasks(tasks)

	// Select task "b" — "a" should be ancestor (cyan), "c" should be descendant (magenta)
	tt.SelectedID = "b"
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = 1 // on a task, not a header

	output := tt.viewFeatureGrouped(120, 30, "test-project")

	// All tasks should be rendered
	if !strings.Contains(output, "Task A") {
		t.Errorf("Expected 'Task A' in output, got: %s", output)
	}
	if !strings.Contains(output, "Task B") {
		t.Errorf("Expected 'Task B' in output, got: %s", output)
	}
	if !strings.Contains(output, "Task C") {
		t.Errorf("Expected 'Task C' in output, got: %s", output)
	}
}

// TestViewFeatureGrouped_NoHighlightWhenOnHeader verifies that no relation
// highlighting occurs when the selection is on a feature header (not a task).
func TestViewFeatureGrouped_NoHighlightWhenOnHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	tasks := []types.ResolvedTask{
		makeTaskWithFeature("a", "Task A", "ready", "high", "feat-1", nil),
		makeTaskWithFeature("b", "Task B", "waiting", "medium", "feat-1", []string{"a"}),
	}
	tt.SetTasks(tasks)

	// Selection on feature header (selectedFeatureTaskIdx = -1)
	tt.SelectedID = ""
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = -1

	// Should not panic and should render without relation highlighting
	output := tt.viewFeatureGrouped(120, 30, "test-project")

	if !strings.Contains(output, "Task A") {
		t.Errorf("Expected 'Task A' in output, got: %s", output)
	}
}

// TestViewNestedGrouped_RelationHighlighting verifies that selecting a task
// in the nested-grouped view computes ancestor/descendant relation maps.
func TestViewNestedGrouped_RelationHighlighting(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(false) // Use nested status+feature view

	// Chain: a <- b <- c (all "blocked" status+classification so they land in same "Blocked" status group)
	tasks := []types.ResolvedTask{
		{ID: "a", Title: "Task A", Classification: "blocked", Status: "blocked", Priority: "high", FeatureID: "feat-1"},
		{ID: "b", Title: "Task B", Classification: "blocked", Status: "blocked", Priority: "medium", FeatureID: "feat-1", DependsOn: []string{"a"}},
		{ID: "c", Title: "Task C", Classification: "blocked", Status: "blocked", Priority: "medium", FeatureID: "feat-1", DependsOn: []string{"b"}},
	}

	tt.tasks = tasks
	tt.statusGroups = GroupTasksByStatusAndFeature(tasks, nil)

	// Expand all groups
	for i := range tt.statusGroups {
		tt.statusGroups[i].Collapsed = false
		for j := range tt.statusGroups[i].Features {
			tt.statusGroups[i].Features[j].Collapsed = false
		}
	}

	// Select task "b"
	tt.SelectedID = "b"
	tt.selectedStatusIdx = 0
	tt.isOnStatusHeader = false
	tt.selectedFeatureIdx = 0
	tt.selectedTaskIdx = 1 // on a task, not a header

	output := tt.viewNestedGrouped(120, 30, "test-project")

	// All tasks should be rendered
	if !strings.Contains(output, "Task A") {
		t.Errorf("Expected 'Task A' in output, got: %s", output)
	}
	if !strings.Contains(output, "Task B") {
		t.Errorf("Expected 'Task B' in output, got: %s", output)
	}
	if !strings.Contains(output, "Task C") {
		t.Errorf("Expected 'Task C' in output, got: %s", output)
	}
}

// TestViewNestedGrouped_NoHighlightWhenOnStatusHeader verifies that no relation
// highlighting occurs when the cursor is on a status header.
func TestViewNestedGrouped_NoHighlightWhenOnStatusHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(false) // Use nested status+feature view

	// All "blocked" status+classification so they land in same "Blocked" status group
	tasks := []types.ResolvedTask{
		{ID: "a", Title: "Task A", Classification: "blocked", Status: "blocked", Priority: "high", FeatureID: "feat-1"},
		{ID: "b", Title: "Task B", Classification: "blocked", Status: "blocked", Priority: "medium", FeatureID: "feat-1", DependsOn: []string{"a"}},
	}

	tt.tasks = tasks
	tt.statusGroups = GroupTasksByStatusAndFeature(tasks, nil)

	// Expand all groups
	for i := range tt.statusGroups {
		tt.statusGroups[i].Collapsed = false
		for j := range tt.statusGroups[i].Features {
			tt.statusGroups[i].Features[j].Collapsed = false
		}
	}

	// Selection on status header
	tt.SelectedID = ""
	tt.selectedStatusIdx = 0
	tt.isOnStatusHeader = true
	tt.selectedTaskIdx = -1

	// Should not panic
	output := tt.viewNestedGrouped(120, 30, "test-project")
	if !strings.Contains(output, "Task A") {
		t.Errorf("Expected 'Task A' in output, got: %s", output)
	}
}

// TestRenderGroupedTaskLineWithTree_AncestorHighlighting verifies that
// renderGroupedTaskLineWithTree applies ancestor/descendant coloring to tree connectors.
func TestRenderGroupedTaskLineWithTree_AncestorHighlighting(t *testing.T) {
	tt := NewTaskTree()

	node := TreeNode{
		Task: types.ResolvedTask{
			ID:             "a",
			Title:          "Ancestor Task",
			Status:         "pending",
			Classification: "ready",
			Priority:       "medium",
		},
	}

	ancestors := map[string]bool{"a": true}
	descendants := map[string]bool{}

	// Render as non-selected, with task "a" being an ancestor
	line := tt.renderGroupedTaskLineWithTree(
		node,
		"    ", // base prefix
		false,  // not last
		false,  // not selected
		nil,    // no multi-select
		false,  // no checkboxes
		"test-project",
		120,
		ancestors,
		descendants,
	)

	// Should still contain the task title
	if !strings.Contains(line, "Ancestor Task") {
		t.Errorf("Expected 'Ancestor Task' in line, got: %s", line)
	}

	// Should contain tree connector (├─)
	if !strings.Contains(line, "├─") {
		t.Errorf("Expected tree connector '├─' in line for non-last child, got: %s", line)
	}
}

// TestRenderGroupedTaskLineWithTree_DescendantHighlighting verifies descendant coloring.
func TestRenderGroupedTaskLineWithTree_DescendantHighlighting(t *testing.T) {
	tt := NewTaskTree()

	node := TreeNode{
		Task: types.ResolvedTask{
			ID:             "c",
			Title:          "Descendant Task",
			Status:         "pending",
			Classification: "waiting",
			Priority:       "medium",
		},
	}

	ancestors := map[string]bool{}
	descendants := map[string]bool{"c": true}

	line := tt.renderGroupedTaskLineWithTree(
		node,
		"    ",
		true, // last child
		false,
		nil,
		false,
		"test-project",
		120,
		ancestors,
		descendants,
	)

	if !strings.Contains(line, "Descendant Task") {
		t.Errorf("Expected 'Descendant Task' in line, got: %s", line)
	}

	// Should contain last-branch connector (└─)
	if !strings.Contains(line, "└─") {
		t.Errorf("Expected tree connector '└─' in line for last child, got: %s", line)
	}
}

// TestRenderGroupedTaskLineWithTree_NilMapsNoHighlighting verifies nil maps are safe.
func TestRenderGroupedTaskLineWithTree_NilMapsNoHighlighting(t *testing.T) {
	tt := NewTaskTree()

	node := TreeNode{
		Task: types.ResolvedTask{
			ID:             "x",
			Title:          "Normal Task",
			Status:         "pending",
			Classification: "ready",
			Priority:       "medium",
		},
	}

	// nil ancestors and descendants (no highlighting)
	line := tt.renderGroupedTaskLineWithTree(
		node,
		"    ",
		true,
		false,
		nil,
		false,
		"test-project",
		120,
		nil, nil,
	)

	if !strings.Contains(line, "Normal Task") {
		t.Errorf("Expected 'Normal Task' in line, got: %s", line)
	}
}

// TestRenderGroupedTaskLineWithTree_SelectedOverridesRelationColor verifies
// that when a task is both selected and an ancestor/descendant, the selected
// blue background style takes precedence over relation highlighting.
func TestRenderGroupedTaskLineWithTree_SelectedOverridesRelationColor(t *testing.T) {
	tt := NewTaskTree()
	tt.selectedTaskIdx = 0 // must be >= 0 for isSelected to trigger

	node := TreeNode{
		Task: types.ResolvedTask{
			ID:             "a",
			Title:          "Selected Ancestor",
			Status:         "pending",
			Classification: "ready",
			Priority:       "medium",
		},
	}

	ancestors := map[string]bool{"a": true}

	// Render as selected AND ancestor
	line := tt.renderGroupedTaskLineWithTree(
		node,
		"    ",
		true,
		true, // isSelected
		nil,
		false,
		"test-project",
		120,
		ancestors,
		nil,
	)

	// Should still render the task (selected style overrides)
	if !strings.Contains(line, "Selected Ancestor") {
		t.Errorf("Expected 'Selected Ancestor' in line, got: %s", line)
	}
}

// TestFindSelectedLineInFeatureView_MixedStatusFeatures verifies that viewport scroll
// targeting uses the same filtered feature list as rendering. When some features contain
// only non-active tasks (draft/completed), they are excluded from activeFeatureGroups
// but still present in the unfiltered featureGroups.Features. The selected line
// computation must use activeFeatureGroups to avoid desync.
func TestFindSelectedLineInFeatureView_MixedStatusFeatures(t *testing.T) {
	// Feature "alpha" has only draft tasks → excluded from activeFeatureGroups
	// Feature "beta" has active tasks → included in activeFeatureGroups
	// Feature "gamma" has only completed tasks → excluded from activeFeatureGroups
	// Feature "delta" has active tasks → included in activeFeatureGroups

	allTasks := []types.ResolvedTask{
		{ID: "a1", Title: "Alpha Draft", Status: "draft", FeatureID: "alpha", Priority: "medium"},
		{ID: "b1", Title: "Beta Task 1", Status: "pending", Classification: "ready", FeatureID: "beta", Priority: "high"},
		{ID: "b2", Title: "Beta Task 2", Status: "pending", Classification: "ready", FeatureID: "beta", Priority: "medium"},
		{ID: "g1", Title: "Gamma Done", Status: "completed", FeatureID: "gamma", Priority: "low"},
		{ID: "d1", Title: "Delta Task 1", Status: "in_progress", FeatureID: "delta", Priority: "high"},
	}

	// activeFeatureGroups: only features with active tasks (beta, delta)
	activeFeatureGroups := []FeatureGroup{
		{
			ID:   "beta",
			Name: "beta",
			Tasks: []types.ResolvedTask{
				allTasks[1], // b1
				allTasks[2], // b2
			},
		},
		{
			ID:   "delta",
			Name: "delta",
			Tasks: []types.ResolvedTask{
				allTasks[4], // d1
			},
		},
	}

	draftTasks := []types.ResolvedTask{allTasks[0]}
	completedTasks := []types.ResolvedTask{allTasks[3]}
	noTasks := []terminalSectionLineInfo{
		{tasks: draftTasks, isOn: false, collapsed: false, featureIdx: -1, taskIdx: -1, featureIDs: nil},
		{tasks: nil, isOn: false, collapsed: true, featureIdx: -1, taskIdx: -1, featureIDs: nil},
		{tasks: nil, isOn: false, collapsed: true, featureIdx: -1, taskIdx: -1, featureIDs: nil},
		{tasks: nil, isOn: false, collapsed: true, featureIdx: -1, taskIdx: -1, featureIDs: nil},
		{tasks: completedTasks, isOn: false, collapsed: false, featureIdx: -1, taskIdx: -1, featureIDs: nil},
	}

	// Test 1: Selecting beta feature header (the first active feature, line 0)
	lineIdx := findSelectedLineInFeatureView(
		activeFeatureGroups, nil, allTasks,
		"",
		"beta", -1, false, false, noTasks,
	)
	if lineIdx != 0 {
		t.Errorf("Expected beta header at line 0, got %d", lineIdx)
	}

	// Test 2: Selecting first task in beta (b1, line 1 = after header)
	lineIdx = findSelectedLineInFeatureView(
		activeFeatureGroups, nil, allTasks,
		"",
		"beta", 0, false, false, noTasks,
	)
	if lineIdx != 1 {
		t.Errorf("Expected first beta task at line 1, got %d", lineIdx)
	}

	// Test 3: Selecting delta feature header (line 3 = beta header + 2 beta tasks + delta header)
	lineIdx = findSelectedLineInFeatureView(
		activeFeatureGroups, nil, allTasks,
		"",
		"delta", -1, false, false, noTasks,
	)
	if lineIdx != 3 {
		t.Errorf("Expected delta header at line 3, got %d", lineIdx)
	}

	// Test 4: Selecting delta's first task (line 4 = after delta header)
	lineIdx = findSelectedLineInFeatureView(
		activeFeatureGroups, nil, allTasks,
		"",
		"delta", 0, false, false, noTasks,
	)
	if lineIdx != 4 {
		t.Errorf("Expected first delta task at line 4, got %d", lineIdx)
	}
}

// TestFindSelectedLineInFeatureView_DraftAndCompletedSections verifies that
// draft and completed section headers are correctly located.
func TestFindSelectedLineInFeatureView_DraftAndCompletedSections(t *testing.T) {
	// One active feature with 1 task = 2 lines (header + task)
	activeFeatureGroups := []FeatureGroup{
		{
			ID:   "feat-a",
			Name: "feat-a",
			Tasks: []types.ResolvedTask{
				{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", FeatureID: "feat-a"},
			},
		},
	}
	allTasks := activeFeatureGroups[0].Tasks
	draftTasks := []types.ResolvedTask{
		{ID: "d1", Title: "Draft 1", Status: "draft", FeatureID: "feat-a"},
	}
	completedTasks := []types.ResolvedTask{
		{ID: "c1", Title: "Done 1", Status: "completed", FeatureID: "feat-a"},
	}

	// Draft section header should be at line: 2 (feature header + 1 task) + 1 (blank line) = line 3
	termSections := []terminalSectionLineInfo{
		{tasks: draftTasks, isOn: true, collapsed: false, featureIdx: -1, taskIdx: -1, featureIDs: nil},
		{tasks: nil, isOn: false, collapsed: true, featureIdx: -1, taskIdx: -1, featureIDs: nil},
		{tasks: nil, isOn: false, collapsed: true, featureIdx: -1, taskIdx: -1, featureIDs: nil},
		{tasks: nil, isOn: false, collapsed: true, featureIdx: -1, taskIdx: -1, featureIDs: nil},
		{tasks: completedTasks, isOn: false, collapsed: false, featureIdx: -1, taskIdx: -1, featureIDs: nil},
	}
	lineIdx := findSelectedLineInFeatureView(
		activeFeatureGroups, nil, allTasks,
		"",
		"", -1, false, true, termSections, // isOnAnyTerminal=true
	)
	if lineIdx != 3 {
		t.Errorf("Expected draft section header at line 3, got %d", lineIdx)
	}
}
