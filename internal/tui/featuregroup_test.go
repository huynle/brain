package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestGroupTasksByFeature_Empty tests grouping with no tasks.
func TestGroupTasksByFeature_Empty(t *testing.T) {
	result := GroupTasksByFeature(nil)

	if len(result.Features) != 0 {
		t.Errorf("Expected 0 features, got %d", len(result.Features))
	}
	if result.Ungrouped != nil {
		t.Errorf("Expected nil ungrouped, got %+v", result.Ungrouped)
	}
}

// TestGroupTasksByFeature_AllUngrouped tests tasks without feature_id.
func TestGroupTasksByFeature_AllUngrouped(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", FeatureID: "", Priority: "high"},
		{ID: "task2", Title: "Task 2", FeatureID: "", Priority: "low"},
	}

	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 0 {
		t.Errorf("Expected 0 features, got %d", len(result.Features))
	}
	if result.Ungrouped == nil {
		t.Fatalf("Expected ungrouped group to exist")
	}
	if len(result.Ungrouped.Tasks) != 2 {
		t.Errorf("Expected 2 ungrouped tasks, got %d", len(result.Ungrouped.Tasks))
	}
	if result.Ungrouped.Name != "[Ungrouped]" {
		t.Errorf("Expected ungrouped name '[Ungrouped]', got %q", result.Ungrouped.Name)
	}
}

// TestGroupTasksByFeature_ByFeatureID tests grouping by feature_id.
func TestGroupTasksByFeature_ByFeatureID(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Auth task 1", FeatureID: "auth-system", Priority: "high"},
		{ID: "task2", Title: "Auth task 2", FeatureID: "auth-system", Priority: "low"},
		{ID: "task3", Title: "Dashboard task", FeatureID: "dashboard", Priority: "medium"},
		{ID: "task4", Title: "Ungrouped task", FeatureID: "", Priority: "medium"},
	}

	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 2 {
		t.Fatalf("Expected 2 features, got %d", len(result.Features))
	}

	// Check auth-system feature
	authFeature := findFeature(result.Features, "auth-system")
	if authFeature == nil {
		t.Fatalf("Expected auth-system feature to exist")
	}
	if len(authFeature.Tasks) != 2 {
		t.Errorf("Expected 2 tasks in auth-system, got %d", len(authFeature.Tasks))
	}

	// Check dashboard feature
	dashFeature := findFeature(result.Features, "dashboard")
	if dashFeature == nil {
		t.Fatalf("Expected dashboard feature to exist")
	}
	if len(dashFeature.Tasks) != 1 {
		t.Errorf("Expected 1 task in dashboard, got %d", len(dashFeature.Tasks))
	}

	// Check ungrouped
	if result.Ungrouped == nil {
		t.Fatalf("Expected ungrouped group to exist")
	}
	if len(result.Ungrouped.Tasks) != 1 {
		t.Errorf("Expected 1 ungrouped task, got %d", len(result.Ungrouped.Tasks))
	}
}

// TestGroupTasksByFeature_Sorting tests feature sorting by priority and name.
func TestGroupTasksByFeature_Sorting(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", FeatureID: "zzz-feature", FeaturePriority: "low"},
		{ID: "task2", FeatureID: "aaa-feature", FeaturePriority: "medium"},
		{ID: "task3", FeatureID: "bbb-feature", FeaturePriority: "high"},
		{ID: "task4", FeatureID: "ccc-feature", FeaturePriority: "high"},
	}

	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 4 {
		t.Fatalf("Expected 4 features, got %d", len(result.Features))
	}

	// Check ordering: high priority first (alphabetically), then medium, then low
	expected := []string{"bbb-feature", "ccc-feature", "aaa-feature", "zzz-feature"}
	for i, featureID := range expected {
		if result.Features[i].ID != featureID {
			t.Errorf("Expected feature[%d] to be %s, got %s", i, featureID, result.Features[i].ID)
		}
	}
}

// TestFeatureStats tests stat calculation for features.
func TestFeatureStats(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "completed", Classification: "ready"},
		{ID: "t2", Status: "in_progress", Classification: "ready"},
		{ID: "t3", Status: "pending", Classification: "waiting"},
		{ID: "t4", Status: "pending", Classification: "blocked"},
	}

	stats := computeFeatureStats(tasks)

	if stats.Total != 4 {
		t.Errorf("Expected total=4, got %d", stats.Total)
	}
	if stats.Completed != 1 {
		t.Errorf("Expected completed=1, got %d", stats.Completed)
	}
	if stats.Active != 1 {
		t.Errorf("Expected active=1, got %d", stats.Active)
	}
	if stats.Ready != 2 {
		t.Errorf("Expected ready=2, got %d", stats.Ready)
	}
	if stats.Waiting != 1 {
		t.Errorf("Expected waiting=1, got %d", stats.Waiting)
	}
	if stats.Blocked != 1 {
		t.Errorf("Expected blocked=1, got %d", stats.Blocked)
	}
}

// TestAggregateFeatureStatusIcon tests the aggregated status icon logic.
func TestAggregateFeatureStatusIcon(t *testing.T) {
	tests := []struct {
		name             string
		tasks            []types.ResolvedTask
		expectedIcon     string
		expectedIsActive bool
	}{
		{
			name: "in_progress takes priority",
			tasks: []types.ResolvedTask{
				{ID: "t1", Status: "pending", Classification: "ready"},
				{ID: "t2", Status: "in_progress", Classification: "ready"},
				{ID: "t3", Status: "pending", Classification: "blocked"},
			},
			expectedIcon:     IndicatorActive, // ▶
			expectedIsActive: true,
		},
		{
			name: "active status treated as in_progress",
			tasks: []types.ResolvedTask{
				{ID: "t1", Status: "active", Classification: "ready"},
				{ID: "t2", Status: "pending", Classification: "waiting"},
			},
			expectedIcon:     IndicatorActive, // ▶
			expectedIsActive: true,
		},
		{
			name: "blocked when no in_progress",
			tasks: []types.ResolvedTask{
				{ID: "t1", Status: "pending", Classification: "blocked"},
				{ID: "t2", Status: "pending", Classification: "ready"},
			},
			expectedIcon:     IndicatorBlocked, // ✗
			expectedIsActive: false,
		},
		{
			name: "ready when no in_progress or blocked",
			tasks: []types.ResolvedTask{
				{ID: "t1", Status: "pending", Classification: "ready"},
				{ID: "t2", Status: "pending", Classification: "ready"},
			},
			expectedIcon:     IndicatorReady, // ●
			expectedIsActive: false,
		},
		{
			name: "waiting when no in_progress, blocked, or ready",
			tasks: []types.ResolvedTask{
				{ID: "t1", Status: "pending", Classification: "waiting"},
				{ID: "t2", Status: "pending", Classification: "waiting"},
			},
			expectedIcon:     IndicatorWaiting, // ○
			expectedIsActive: false,
		},
		{
			name: "completed when all tasks completed",
			tasks: []types.ResolvedTask{
				{ID: "t1", Status: "completed", Classification: ""},
				{ID: "t2", Status: "validated", Classification: ""},
			},
			expectedIcon:     IndicatorCompleted, // ✓
			expectedIsActive: false,
		},
		{
			name: "completed with cancelled and archived",
			tasks: []types.ResolvedTask{
				{ID: "t1", Status: "completed", Classification: ""},
				{ID: "t2", Status: "cancelled", Classification: ""},
				{ID: "t3", Status: "archived", Classification: ""},
			},
			expectedIcon:     IndicatorCompleted, // ✓
			expectedIsActive: false,
		},
		{
			name:             "empty tasks defaults to ready",
			tasks:            []types.ResolvedTask{},
			expectedIcon:     IndicatorReady, // ●
			expectedIsActive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			icon, isActive := aggregateFeatureStatusIcon(tt.tasks)
			if icon != tt.expectedIcon {
				t.Errorf("Expected icon %q, got %q", tt.expectedIcon, icon)
			}
			if isActive != tt.expectedIsActive {
				t.Errorf("Expected isActive=%v, got %v", tt.expectedIsActive, isActive)
			}
		})
	}
}

// TestGroupTasksByFeature_DependsOn tests that DependsOn is populated from task FeatureDependsOn.
func TestGroupTasksByFeature_DependsOn(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "task1", FeatureID: "feat-auth", FeatureDependsOn: []string{"feat-db", "feat-config"}},
		{ID: "task2", FeatureID: "feat-auth", FeatureDependsOn: []string{"feat-db", "feat-config"}},
		{ID: "task3", FeatureID: "feat-dashboard"},
		{ID: "task4", FeatureID: ""},
	}

	result := GroupTasksByFeature(tasks)

	// Feature with deps should have DependsOn populated
	authFeature := findFeature(result.Features, "feat-auth")
	if authFeature == nil {
		t.Fatalf("Expected feat-auth feature to exist")
	}
	if len(authFeature.DependsOn) != 2 {
		t.Fatalf("Expected DependsOn length 2, got %d", len(authFeature.DependsOn))
	}
	if authFeature.DependsOn[0] != "feat-db" {
		t.Errorf("Expected DependsOn[0]='feat-db', got %q", authFeature.DependsOn[0])
	}
	if authFeature.DependsOn[1] != "feat-config" {
		t.Errorf("Expected DependsOn[1]='feat-config', got %q", authFeature.DependsOn[1])
	}

	// Feature without deps should have nil DependsOn
	dashFeature := findFeature(result.Features, "feat-dashboard")
	if dashFeature == nil {
		t.Fatalf("Expected feat-dashboard feature to exist")
	}
	if len(dashFeature.DependsOn) != 0 {
		t.Errorf("Expected empty DependsOn for feat-dashboard, got %v", dashFeature.DependsOn)
	}

	// Ungrouped should have nil DependsOn
	if result.Ungrouped == nil {
		t.Fatalf("Expected ungrouped group to exist")
	}
	if len(result.Ungrouped.DependsOn) != 0 {
		t.Errorf("Expected empty DependsOn for ungrouped, got %v", result.Ungrouped.DependsOn)
	}
}

// TestComputeTopologicalDepth tests the topological depth computation.
func TestComputeTopologicalDepth(t *testing.T) {
	tests := []struct {
		name     string
		depsMap  map[string][]string
		query    string
		expected int
	}{
		{
			name:     "root feature (no deps) has depth 0",
			depsMap:  map[string][]string{"A": {}},
			query:    "A",
			expected: 0,
		},
		{
			name:     "single dependency has depth 1",
			depsMap:  map[string][]string{"A": {}, "B": {"A"}},
			query:    "B",
			expected: 1,
		},
		{
			name:     "chain A->B->C has depth 2 for C",
			depsMap:  map[string][]string{"A": {}, "B": {"A"}, "C": {"B"}},
			query:    "C",
			expected: 2,
		},
		{
			name:     "diamond: D depends on B and C which both depend on A",
			depsMap:  map[string][]string{"A": {}, "B": {"A"}, "C": {"A"}, "D": {"B", "C"}},
			query:    "D",
			expected: 2,
		},
		{
			name:     "cycle returns negative sentinel",
			depsMap:  map[string][]string{"A": {"B"}, "B": {"A"}},
			query:    "A",
			expected: -1, // raw function returns -1; caller clamps to 0
		},
		{
			name:     "unknown feature has depth 0",
			depsMap:  map[string][]string{"A": {}},
			query:    "nonexistent",
			expected: 0,
		},
		{
			name:     "deep chain depth 4",
			depsMap:  map[string][]string{"A": {}, "B": {"A"}, "C": {"B"}, "D": {"C"}, "E": {"D"}},
			query:    "E",
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			visited := make(map[string]bool)
			depth := computeTopologicalDepth(tt.query, tt.depsMap, visited)
			if depth != tt.expected {
				t.Errorf("computeTopologicalDepth(%q) = %d, want %d", tt.query, depth, tt.expected)
			}
		})
	}
}

// TestGroupTasksByFeature_TopologicalSort tests that features are sorted by
// priority first, then topological depth, then alphabetically.
func TestGroupTasksByFeature_TopologicalSort(t *testing.T) {
	// Setup: A has no deps, B->A, C->A, D->B+C (diamond pattern)
	// All same priority. Expected order: A (depth 0), B (depth 1), C (depth 1), D (depth 2)
	tasks := []types.ResolvedTask{
		{ID: "t-d", FeatureID: "D", FeaturePriority: "medium", FeatureDependsOn: []string{"B", "C"}},
		{ID: "t-b", FeatureID: "B", FeaturePriority: "medium", FeatureDependsOn: []string{"A"}},
		{ID: "t-c", FeatureID: "C", FeaturePriority: "medium", FeatureDependsOn: []string{"A"}},
		{ID: "t-a", FeatureID: "A", FeaturePriority: "medium"},
	}

	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 4 {
		t.Fatalf("Expected 4 features, got %d", len(result.Features))
	}

	// Expected: A (depth 0), B (depth 1), C (depth 1), D (depth 2)
	expected := []string{"A", "B", "C", "D"}
	for i, featureID := range expected {
		if result.Features[i].ID != featureID {
			t.Errorf("Features[%d] = %s, want %s (full order: %v)",
				i, result.Features[i].ID, featureID, featureIDs(result.Features))
		}
	}
}

// TestGroupTasksByFeature_PriorityOverTopology tests that priority still
// takes precedence over topological depth.
func TestGroupTasksByFeature_PriorityOverTopology(t *testing.T) {
	// High-priority feature depends on low-priority feature,
	// but should still sort first because priority wins.
	tasks := []types.ResolvedTask{
		{ID: "t-low", FeatureID: "low-root", FeaturePriority: "low"},
		{ID: "t-high", FeatureID: "high-child", FeaturePriority: "high", FeatureDependsOn: []string{"low-root"}},
	}

	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 2 {
		t.Fatalf("Expected 2 features, got %d", len(result.Features))
	}

	// high-child should come first despite being depth 1
	expected := []string{"high-child", "low-root"}
	for i, featureID := range expected {
		if result.Features[i].ID != featureID {
			t.Errorf("Features[%d] = %s, want %s", i, result.Features[i].ID, featureID)
		}
	}
}

// TestGroupTasksByFeature_DeepChain tests correct ordering of a deep dependency chain.
func TestGroupTasksByFeature_DeepChain(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t-d", FeatureID: "D", FeaturePriority: "medium", FeatureDependsOn: []string{"C"}},
		{ID: "t-a", FeatureID: "A", FeaturePriority: "medium"},
		{ID: "t-c", FeatureID: "C", FeaturePriority: "medium", FeatureDependsOn: []string{"B"}},
		{ID: "t-b", FeatureID: "B", FeaturePriority: "medium", FeatureDependsOn: []string{"A"}},
	}

	result := GroupTasksByFeature(tasks)

	expected := []string{"A", "B", "C", "D"}
	for i, featureID := range expected {
		if result.Features[i].ID != featureID {
			t.Errorf("Features[%d] = %s, want %s (full order: %v)",
				i, result.Features[i].ID, featureID, featureIDs(result.Features))
		}
	}
}

// TestGroupTasksByFeature_CycleDoesNotCrash tests that cycles don't cause infinite recursion.
func TestGroupTasksByFeature_CycleDoesNotCrash(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t-a", FeatureID: "A", FeaturePriority: "medium", FeatureDependsOn: []string{"B"}},
		{ID: "t-b", FeatureID: "B", FeaturePriority: "medium", FeatureDependsOn: []string{"A"}},
	}

	// Should not panic or infinite loop
	result := GroupTasksByFeature(tasks)

	if len(result.Features) != 2 {
		t.Fatalf("Expected 2 features, got %d", len(result.Features))
	}
	// Both get depth 0 due to cycle, so sorted alphabetically
	if result.Features[0].ID != "A" || result.Features[1].ID != "B" {
		t.Errorf("Expected cycle members sorted alphabetically: A, B; got %s, %s",
			result.Features[0].ID, result.Features[1].ID)
	}
}

// TestFeatureDepStatusIcon tests the featureDepStatusIcon helper function.
func TestFeatureDepStatusIcon(t *testing.T) {
	tests := []struct {
		name         string
		depFeatureID string
		allFeatures  []FeatureGroup
		expected     string
	}{
		{
			name:         "returns ? for unknown feature",
			depFeatureID: "nonexistent",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{{ID: "t1", Status: "pending"}}},
			},
			expected: "?",
		},
		{
			name:         "returns ? for empty allFeatures",
			depFeatureID: "feat-a",
			allFeatures:  []FeatureGroup{},
			expected:     "?",
		},
		{
			name:         "returns ✓ when all tasks completed",
			depFeatureID: "feat-a",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{
					{ID: "t1", Status: "completed"},
					{ID: "t2", Status: "validated"},
				}},
			},
			expected: IndicatorCompleted,
		},
		{
			name:         "returns ✓ with cancelled/archived/superseded tasks",
			depFeatureID: "feat-a",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{
					{ID: "t1", Status: "completed"},
					{ID: "t2", Status: "cancelled"},
					{ID: "t3", Status: "archived"},
					{ID: "t4", Status: "superseded"},
				}},
			},
			expected: IndicatorCompleted,
		},
		{
			name:         "returns ▶ when has in_progress tasks",
			depFeatureID: "feat-a",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{
					{ID: "t1", Status: "in_progress", Classification: "ready"},
					{ID: "t2", Status: "pending", Classification: "blocked"},
				}},
			},
			expected: IndicatorActive,
		},
		{
			name:         "returns ▶ when has active tasks",
			depFeatureID: "feat-a",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{
					{ID: "t1", Status: "active", Classification: "ready"},
					{ID: "t2", Status: "pending", Classification: "waiting"},
				}},
			},
			expected: IndicatorActive,
		},
		{
			name:         "returns ✗ when has blocked tasks and none in_progress",
			depFeatureID: "feat-a",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{
					{ID: "t1", Status: "pending", Classification: "blocked"},
					{ID: "t2", Status: "pending", Classification: "waiting"},
				}},
			},
			expected: IndicatorBlocked,
		},
		{
			name:         "returns ○ when pending/waiting only",
			depFeatureID: "feat-a",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{
					{ID: "t1", Status: "pending", Classification: "waiting"},
					{ID: "t2", Status: "pending", Classification: "waiting"},
				}},
			},
			expected: IndicatorWaiting,
		},
		{
			name:         "returns ○ for feature with empty task list",
			depFeatureID: "feat-a",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{}},
			},
			expected: IndicatorWaiting,
		},
		{
			name:         "finds correct feature among multiple",
			depFeatureID: "feat-b",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{
					{ID: "t1", Status: "completed"},
				}},
				{ID: "feat-b", Tasks: []types.ResolvedTask{
					{ID: "t2", Status: "pending", Classification: "blocked"},
				}},
				{ID: "feat-c", Tasks: []types.ResolvedTask{
					{ID: "t3", Status: "in_progress"},
				}},
			},
			expected: IndicatorBlocked,
		},
		{
			name:         "returns ○ for ready-only tasks with no in_progress or blocked",
			depFeatureID: "feat-a",
			allFeatures: []FeatureGroup{
				{ID: "feat-a", Tasks: []types.ResolvedTask{
					{ID: "t1", Status: "pending", Classification: "ready"},
					{ID: "t2", Status: "completed"},
				}},
			},
			expected: IndicatorWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := featureDepStatusIcon(tt.depFeatureID, tt.allFeatures)
			if got != tt.expected {
				t.Errorf("featureDepStatusIcon(%q) = %q, want %q", tt.depFeatureID, got, tt.expected)
			}
		})
	}
}

// featureIDs extracts IDs from feature groups for debug output.
func featureIDs(features []FeatureGroup) []string {
	ids := make([]string, len(features))
	for i, f := range features {
		ids[i] = f.ID
	}
	return ids
}

// Helper function to find a feature by ID.
func findFeature(features []FeatureGroup, id string) *FeatureGroup {
	for i := range features {
		if features[i].ID == id {
			return &features[i]
		}
	}
	return nil
}
