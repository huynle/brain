package service

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Helper: make a ResolvedTask for feature testing
// ---------------------------------------------------------------------------

func makeResolvedTask(id, status, priority, featureID, featurePriority string, featureDeps []string) types.ResolvedTask {
	return types.ResolvedTask{
		ID:               id,
		Path:             "projects/test/task/" + id + ".md",
		Title:            "Task " + id,
		Status:           status,
		Priority:         priority,
		Created:          "2025-01-01T00:00:00Z",
		FeatureID:        featureID,
		FeaturePriority:  featurePriority,
		FeatureDependsOn: featureDeps,
		Classification:   "ready",
		ResolvedDeps:     []string{},
		UnresolvedDeps:   []string{},
		BlockedBy:        []string{},
		WaitingOn:        []string{},
	}
}

// ---------------------------------------------------------------------------
// ComputeFeatureStatus
// ---------------------------------------------------------------------------

func TestComputeFeatureStatus(t *testing.T) {
	tests := []struct {
		name  string
		tasks []types.ResolvedTask
		want  string
	}{
		{
			name:  "empty tasks",
			tasks: nil,
			want:  "pending",
		},
		{
			name: "all completed",
			tasks: []types.ResolvedTask{
				{Status: "completed"},
				{Status: "validated"},
			},
			want: "completed",
		},
		{
			name: "any in_progress",
			tasks: []types.ResolvedTask{
				{Status: "pending"},
				{Status: "in_progress"},
			},
			want: "in_progress",
		},
		{
			name: "any blocked (no in_progress)",
			tasks: []types.ResolvedTask{
				{Status: "pending"},
				{Status: "blocked"},
			},
			want: "blocked",
		},
		{
			name: "cancelled counts as blocked",
			tasks: []types.ResolvedTask{
				{Status: "pending"},
				{Status: "cancelled"},
			},
			want: "blocked",
		},
		{
			name: "all pending",
			tasks: []types.ResolvedTask{
				{Status: "pending"},
				{Status: "pending"},
			},
			want: "pending",
		},
		{
			name: "in_progress takes precedence over blocked",
			tasks: []types.ResolvedTask{
				{Status: "blocked"},
				{Status: "in_progress"},
			},
			want: "in_progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeFeatureStatus(tt.tasks)
			if got != tt.want {
				t.Errorf("ComputeFeatureStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ComputeFeatures
// ---------------------------------------------------------------------------

func TestComputeFeatures_Empty(t *testing.T) {
	features := ComputeFeatures(nil)
	if len(features) != 0 {
		t.Errorf("expected 0 features, got %d", len(features))
	}
}

func TestComputeFeatures_SkipsTasksWithoutFeatureID(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeResolvedTask("t1", "pending", "high", "", "", nil),
	}
	features := ComputeFeatures(tasks)
	if len(features) != 0 {
		t.Errorf("expected 0 features (no feature_id), got %d", len(features))
	}
}

func TestComputeFeatures_GroupsByFeatureID(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeResolvedTask("t1", "pending", "high", "feat-a", "high", nil),
		makeResolvedTask("t2", "pending", "medium", "feat-a", "high", nil),
		makeResolvedTask("t3", "pending", "low", "feat-b", "medium", nil),
	}
	features := ComputeFeatures(tasks)

	if len(features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(features))
	}

	featureMap := make(map[string]*ComputedFeature)
	for _, f := range features {
		featureMap[f.ID] = f
	}

	if fa, ok := featureMap["feat-a"]; !ok {
		t.Error("expected feature 'feat-a'")
	} else {
		if len(fa.Tasks) != 2 {
			t.Errorf("feat-a tasks = %d, want 2", len(fa.Tasks))
		}
		if fa.TaskStats.Total != 2 {
			t.Errorf("feat-a task_stats.total = %d, want 2", fa.TaskStats.Total)
		}
	}

	if fb, ok := featureMap["feat-b"]; !ok {
		t.Error("expected feature 'feat-b'")
	} else {
		if len(fb.Tasks) != 1 {
			t.Errorf("feat-b tasks = %d, want 1", len(fb.Tasks))
		}
	}
}

func TestComputeFeatures_Priority(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeResolvedTask("t1", "pending", "low", "feat-a", "low", nil),
		makeResolvedTask("t2", "pending", "medium", "feat-a", "high", nil),
	}
	features := ComputeFeatures(tasks)

	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}
	if features[0].Priority != "high" {
		t.Errorf("priority = %q, want 'high'", features[0].Priority)
	}
}

func TestComputeFeatures_CollectsFeatureDeps(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeResolvedTask("t1", "pending", "high", "feat-a", "high", []string{"feat-b"}),
		makeResolvedTask("t2", "pending", "high", "feat-a", "high", []string{"feat-b", "feat-c"}),
	}
	features := ComputeFeatures(tasks)

	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}
	deps := features[0].DependsOnFeatures
	if len(deps) != 2 {
		t.Errorf("depends_on_features = %v, want 2 unique deps", deps)
	}
}

func TestComputeFeatures_TaskStats(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeResolvedTask("t1", "pending", "high", "feat-a", "high", nil),
		makeResolvedTask("t2", "in_progress", "high", "feat-a", "high", nil),
		makeResolvedTask("t3", "completed", "high", "feat-a", "high", nil),
		makeResolvedTask("t4", "blocked", "high", "feat-a", "high", nil),
	}
	features := ComputeFeatures(tasks)

	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}
	stats := features[0].TaskStats
	if stats.Total != 4 {
		t.Errorf("total = %d, want 4", stats.Total)
	}
	if stats.Pending != 1 {
		t.Errorf("pending = %d, want 1", stats.Pending)
	}
	if stats.InProgress != 1 {
		t.Errorf("in_progress = %d, want 1", stats.InProgress)
	}
	if stats.Completed != 1 {
		t.Errorf("completed = %d, want 1", stats.Completed)
	}
	if stats.Blocked != 1 {
		t.Errorf("blocked = %d, want 1", stats.Blocked)
	}
}

// ---------------------------------------------------------------------------
// ResolveFeatureDependencies
// ---------------------------------------------------------------------------

func TestResolveFeatureDependencies_Empty(t *testing.T) {
	result := ResolveFeatureDependencies(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 features, got %d", len(result))
	}
}

func TestResolveFeatureDependencies_NoDeps(t *testing.T) {
	features := []*ComputedFeature{
		{ID: "feat-a", Status: "pending", DependsOnFeatures: nil},
		{ID: "feat-b", Status: "pending", DependsOnFeatures: nil},
	}
	resolved := ResolveFeatureDependencies(features)

	for _, f := range resolved {
		if f.Classification != "ready" {
			t.Errorf("feature %q classification = %q, want 'ready'", f.ID, f.Classification)
		}
	}
}

func TestResolveFeatureDependencies_WithDeps(t *testing.T) {
	features := []*ComputedFeature{
		{ID: "feat-a", Status: "pending", DependsOnFeatures: nil},
		{ID: "feat-b", Status: "pending", DependsOnFeatures: []string{"feat-a"}},
	}
	resolved := ResolveFeatureDependencies(features)

	featureMap := make(map[string]*ComputedFeature)
	for _, f := range resolved {
		featureMap[f.ID] = f
	}

	if featureMap["feat-a"].Classification != "ready" {
		t.Errorf("feat-a classification = %q, want 'ready'", featureMap["feat-a"].Classification)
	}
	if featureMap["feat-b"].Classification != "waiting" {
		t.Errorf("feat-b classification = %q, want 'waiting'", featureMap["feat-b"].Classification)
	}
}

func TestResolveFeatureDependencies_CompletedDepSatisfied(t *testing.T) {
	features := []*ComputedFeature{
		{ID: "feat-a", Status: "completed", DependsOnFeatures: nil},
		{ID: "feat-b", Status: "pending", DependsOnFeatures: []string{"feat-a"}},
	}
	resolved := ResolveFeatureDependencies(features)

	featureMap := make(map[string]*ComputedFeature)
	for _, f := range resolved {
		featureMap[f.ID] = f
	}

	if featureMap["feat-b"].Classification != "ready" {
		t.Errorf("feat-b classification = %q, want 'ready' (dep completed)", featureMap["feat-b"].Classification)
	}
}

func TestResolveFeatureDependencies_Cycle(t *testing.T) {
	features := []*ComputedFeature{
		{ID: "feat-a", Status: "pending", DependsOnFeatures: []string{"feat-b"}},
		{ID: "feat-b", Status: "pending", DependsOnFeatures: []string{"feat-a"}},
	}
	resolved := ResolveFeatureDependencies(features)

	for _, f := range resolved {
		if f.Classification != "blocked" {
			t.Errorf("feature %q classification = %q, want 'blocked' (cycle)", f.ID, f.Classification)
		}
		if !f.InCycle {
			t.Errorf("feature %q in_cycle = false, want true", f.ID)
		}
	}
}

func TestResolveFeatureDependencies_BlockedDep(t *testing.T) {
	features := []*ComputedFeature{
		{ID: "feat-a", Status: "blocked", DependsOnFeatures: nil},
		{ID: "feat-b", Status: "pending", DependsOnFeatures: []string{"feat-a"}},
	}
	resolved := ResolveFeatureDependencies(features)

	featureMap := make(map[string]*ComputedFeature)
	for _, f := range resolved {
		featureMap[f.ID] = f
	}

	if featureMap["feat-b"].Classification != "blocked" {
		t.Errorf("feat-b classification = %q, want 'blocked'", featureMap["feat-b"].Classification)
	}
}

// ---------------------------------------------------------------------------
// SortFeaturesByPriority
// ---------------------------------------------------------------------------

func TestSortFeaturesByPriority(t *testing.T) {
	features := []*ComputedFeature{
		{ID: "low", Priority: "low", TaskStats: FeatureTaskStats{Total: 1}},
		{ID: "high", Priority: "high", TaskStats: FeatureTaskStats{Total: 1}},
		{ID: "med", Priority: "medium", TaskStats: FeatureTaskStats{Total: 1}},
	}

	sorted := SortFeaturesByPriority(features)

	wantOrder := []string{"high", "med", "low"}
	for i, want := range wantOrder {
		if sorted[i].ID != want {
			t.Errorf("sorted[%d].ID = %q, want %q", i, sorted[i].ID, want)
		}
	}
}

func TestSortFeaturesByPriority_SecondaryByCompletionRatio(t *testing.T) {
	features := []*ComputedFeature{
		{ID: "less-done", Priority: "high", TaskStats: FeatureTaskStats{Total: 10, Completed: 3}},
		{ID: "more-done", Priority: "high", TaskStats: FeatureTaskStats{Total: 10, Completed: 7}},
	}

	sorted := SortFeaturesByPriority(features)

	if sorted[0].ID != "more-done" {
		t.Errorf("sorted[0].ID = %q, want 'more-done' (higher completion ratio)", sorted[0].ID)
	}
}

// ---------------------------------------------------------------------------
// GetReadyFeatures
// ---------------------------------------------------------------------------

func TestGetReadyFeatures(t *testing.T) {
	features := []*ComputedFeature{
		{ID: "a", Classification: "ready", Status: "pending", Priority: "low", TaskStats: FeatureTaskStats{Total: 1}},
		{ID: "b", Classification: "waiting", Status: "pending", Priority: "high", TaskStats: FeatureTaskStats{Total: 1}},
		{ID: "c", Classification: "ready", Status: "pending", Priority: "high", TaskStats: FeatureTaskStats{Total: 1}},
		{ID: "d", Classification: "ready", Status: "completed", Priority: "high", TaskStats: FeatureTaskStats{Total: 1}},
	}

	ready := GetReadyFeatures(features)

	if len(ready) != 2 {
		t.Fatalf("expected 2 ready features, got %d", len(ready))
	}
	if ready[0].ID != "c" {
		t.Errorf("ready[0].ID = %q, want 'c' (high priority)", ready[0].ID)
	}
	if ready[1].ID != "a" {
		t.Errorf("ready[1].ID = %q, want 'a' (low priority)", ready[1].ID)
	}
}

// ---------------------------------------------------------------------------
// ComputeAndResolveFeatures
// ---------------------------------------------------------------------------

func TestComputeAndResolveFeatures_Empty(t *testing.T) {
	result := ComputeAndResolveFeatures(nil)
	if len(result.Features) != 0 {
		t.Errorf("expected 0 features, got %d", len(result.Features))
	}
	if result.Stats.Total != 0 {
		t.Errorf("stats.total = %d, want 0", result.Stats.Total)
	}
}

func TestComputeAndResolveFeatures_Integration(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeResolvedTask("t1", "pending", "high", "feat-a", "high", nil),
		makeResolvedTask("t2", "completed", "high", "feat-a", "high", nil),
		makeResolvedTask("t3", "pending", "high", "feat-b", "medium", []string{"feat-a"}),
	}

	result := ComputeAndResolveFeatures(tasks)

	if len(result.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(result.Features))
	}
	if result.Stats.Total != 2 {
		t.Errorf("stats.total = %d, want 2", result.Stats.Total)
	}

	featureMap := make(map[string]*ComputedFeature)
	for _, f := range result.Features {
		featureMap[f.ID] = f
	}

	if featureMap["feat-a"].Classification != "ready" {
		t.Errorf("feat-a classification = %q, want 'ready'", featureMap["feat-a"].Classification)
	}
	if featureMap["feat-b"].Classification != "waiting" {
		t.Errorf("feat-b classification = %q, want 'waiting'", featureMap["feat-b"].Classification)
	}
}
