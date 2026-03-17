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
