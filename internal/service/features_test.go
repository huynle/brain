package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
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

func insertFeatureAssignmentRunnerForFeatureTest(t *testing.T, store *storage.StorageLayer, runnerID string, lastHeartbeat int64) {
	t.Helper()
	if err := store.UpsertRunner(context.Background(), &storage.RunnerRow{
		RunnerID:      runnerID,
		Hostname:      runnerID + "-host",
		Labels:        map[string]string{},
		Executors:     []string{"opencode"},
		Capabilities:  []string{},
		MaxParallel:   1,
		RegisteredAt:  time.Now().UnixMilli(),
		LastHeartbeat: lastHeartbeat,
		Status:        string(types.RunnerStatusOnline),
	}); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}
}

func TestAssignFeatureToRunner_AssignsOnlineRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	now := time.Now().UnixMilli()
	insertFeatureAssignmentRunnerForFeatureTest(t, store, "runner-1", now)

	resp, err := svc.AssignFeatureToRunner(context.Background(), "brain-api", "feature-1", types.FeatureAssignmentRequest{
		RunnerID: "runner-1",
		Intent:   "assign",
	})
	if err != nil {
		t.Fatalf("AssignFeatureToRunner failed: %v", err)
	}
	if resp.ProjectID != "brain-api" || resp.FeatureID != "feature-1" || resp.RunnerID != "runner-1" {
		t.Fatalf("unexpected assignment response: %+v", resp)
	}
	if resp.Source != "manual" || resp.Status != "active" {
		t.Fatalf("source/status = %q/%q, want manual/active", resp.Source, resp.Status)
	}
	if resp.AssignedAt == "" || resp.UpdatedAt == "" {
		t.Fatalf("expected timestamps in response: %+v", resp)
	}

	row, err := store.GetFeatureAssignment(context.Background(), "brain-api", "feature-1")
	if err != nil {
		t.Fatalf("GetFeatureAssignment failed: %v", err)
	}
	if row == nil || row.RunnerID != "runner-1" || row.Source != "manual" || row.Status != "active" {
		t.Fatalf("unexpected stored assignment: %+v", row)
	}
}

func TestAssignFeatureToRunner_ReassignRequiresExplicitIntent(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	now := time.Now().UnixMilli()
	insertFeatureAssignmentRunnerForFeatureTest(t, store, "runner-1", now)
	insertFeatureAssignmentRunnerForFeatureTest(t, store, "runner-2", now)

	_, err := svc.AssignFeatureToRunner(context.Background(), "brain-api", "feature-1", types.FeatureAssignmentRequest{RunnerID: "runner-1", Intent: "assign"})
	if err != nil {
		t.Fatalf("initial assignment failed: %v", err)
	}

	_, err = svc.AssignFeatureToRunner(context.Background(), "brain-api", "feature-1", types.FeatureAssignmentRequest{RunnerID: "runner-2", Intent: "assign"})
	if !errors.Is(err, api.ErrConflict) {
		t.Fatalf("reassign without explicit intent error = %v, want ErrConflict", err)
	}

	resp, err := svc.AssignFeatureToRunner(context.Background(), "brain-api", "feature-1", types.FeatureAssignmentRequest{RunnerID: "runner-2", Intent: "reassign"})
	if err != nil {
		t.Fatalf("explicit reassign failed: %v", err)
	}
	if resp.RunnerID != "runner-2" || resp.PreviousRunner != "runner-1" {
		t.Fatalf("unexpected reassign response: %+v", resp)
	}
}

func TestAssignFeatureToRunner_MissingRunner(t *testing.T) {
	svc, _, _ := newTestTaskService(t)

	_, err := svc.AssignFeatureToRunner(context.Background(), "brain-api", "feature-1", types.FeatureAssignmentRequest{RunnerID: "missing-runner", Intent: "assign"})
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("AssignFeatureToRunner error = %v, want ErrNotFound", err)
	}
}

func TestAssignFeatureToRunner_OfflineRunnerRequiresForce(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	offlineHeartbeat := time.Now().Add(-RunnerStaleThreshold - time.Minute).UnixMilli()
	insertFeatureAssignmentRunnerForFeatureTest(t, store, "offline-runner", offlineHeartbeat)

	_, err := svc.AssignFeatureToRunner(context.Background(), "brain-api", "feature-1", types.FeatureAssignmentRequest{RunnerID: "offline-runner", Intent: "assign"})
	if !errors.Is(err, api.ErrConflict) {
		t.Fatalf("offline assignment error = %v, want ErrConflict", err)
	}

	resp, err := svc.AssignFeatureToRunner(context.Background(), "brain-api", "feature-1", types.FeatureAssignmentRequest{RunnerID: "offline-runner", Intent: "assign", Force: true})
	if err != nil {
		t.Fatalf("forced offline assignment failed: %v", err)
	}
	if resp.RunnerID != "offline-runner" || resp.Status != "active" {
		t.Fatalf("unexpected forced assignment response: %+v", resp)
	}
}

func TestClearFeatureAssignment_RequiresExplicitIntent(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	now := time.Now().UnixMilli()
	insertFeatureAssignmentRunnerForFeatureTest(t, store, "runner-1", now)
	_, err := svc.AssignFeatureToRunner(context.Background(), "brain-api", "feature-1", types.FeatureAssignmentRequest{RunnerID: "runner-1", Intent: "assign"})
	if err != nil {
		t.Fatalf("initial assignment failed: %v", err)
	}

	_, err = svc.ClearFeatureAssignment(context.Background(), "brain-api", "feature-1", types.ClearFeatureAssignmentRequest{})
	if !errors.Is(err, api.ErrConflict) {
		t.Fatalf("clear without intent error = %v, want ErrConflict", err)
	}

	resp, err := svc.ClearFeatureAssignment(context.Background(), "brain-api", "feature-1", types.ClearFeatureAssignmentRequest{Intent: "clear"})
	if err != nil {
		t.Fatalf("ClearFeatureAssignment failed: %v", err)
	}
	if resp.Status != "cleared" || resp.PreviousRunner != "runner-1" || resp.Source != "manual" {
		t.Fatalf("unexpected clear response: %+v", resp)
	}
	if resp.AssignedAt == "" || resp.UpdatedAt == "" {
		t.Fatalf("expected timestamps in clear response: %+v", resp)
	}

	row, err := store.GetFeatureAssignment(context.Background(), "brain-api", "feature-1")
	if err != nil {
		t.Fatalf("GetFeatureAssignment failed: %v", err)
	}
	if row != nil {
		t.Fatalf("assignment should be cleared, got %+v", row)
	}
}

// ─── TransitiveDependents ────────────────────────────────────────

// feat builds a ComputedFeature for closure tests. Only the fields the
// reverse walk reads are set.
func feat(id string, dependsOn []string, status string) *ComputedFeature {
	return &ComputedFeature{ID: id, DependsOnFeatures: dependsOn, Status: status}
}

func TestTransitiveDependents_Chain(t *testing.T) {
	// A <- B <- C: running A should enrol B then C, in that order, so a
	// caller dispatching in order never runs C before B.
	fs := []*ComputedFeature{
		feat("a", nil, "pending"),
		feat("b", []string{"a"}, "pending"),
		feat("c", []string{"b"}, "pending"),
	}
	got := TransitiveDependents(fs, "a")
	if !reflect.DeepEqual(got.Members, []string{"b", "c"}) {
		t.Fatalf("members = %v, want [b c]", got.Members)
	}
}

func TestTransitiveDependents_RootIsNeverAMember(t *testing.T) {
	fs := []*ComputedFeature{feat("a", nil, "pending"), feat("b", []string{"a"}, "pending")}
	for _, id := range TransitiveDependents(fs, "a").Members {
		if id == "a" {
			t.Fatal("root enrolled as its own dependent; it is dispatched directly")
		}
	}
}

func TestTransitiveDependents_DiamondEnrolsOnce(t *testing.T) {
	// B and C both depend on A; D depends on both. D must appear exactly
	// once, or it would be dispatched twice.
	fs := []*ComputedFeature{
		feat("a", nil, "pending"),
		feat("b", []string{"a"}, "pending"),
		feat("c", []string{"a"}, "pending"),
		feat("d", []string{"b", "c"}, "pending"),
	}
	got := TransitiveDependents(fs, "a")
	n := 0
	for _, id := range got.Members {
		if id == "d" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("d enrolled %d times, want exactly 1 (members=%v)", n, got.Members)
	}
}

func TestTransitiveDependents_UnrelatedExcluded(t *testing.T) {
	fs := []*ComputedFeature{
		feat("a", nil, "pending"),
		feat("b", []string{"a"}, "pending"),
		feat("z", nil, "pending"),
	}
	for _, id := range TransitiveDependents(fs, "a").Members {
		if id == "z" {
			t.Fatal("enrolled a feature that does not depend on the root")
		}
	}
}

func TestTransitiveDependents_SkipsCycleMembers(t *testing.T) {
	// applyFeatureGating blocks a cycle member's tasks unconditionally, so
	// enrolling one guarantees a chain that never finishes.
	b := feat("b", []string{"a"}, "pending")
	b.InCycle = true
	got := TransitiveDependents([]*ComputedFeature{feat("a", nil, "pending"), b}, "a")
	if len(got.Members) != 0 {
		t.Fatalf("members = %v, want none", got.Members)
	}
	if got.Skipped["b"] != "in_cycle" {
		t.Fatalf("skipped[b] = %q, want in_cycle — a silent skip is the failure mode here", got.Skipped["b"])
	}
}

func TestTransitiveDependents_SkipsSettledFeatures(t *testing.T) {
	fs := []*ComputedFeature{
		feat("a", nil, "pending"),
		feat("b", []string{"a"}, "completed"),
	}
	got := TransitiveDependents(fs, "a")
	if len(got.Members) != 0 {
		t.Fatalf("members = %v, want none for an already-completed dependent", got.Members)
	}
	if got.Skipped["b"] != "already_settled" {
		t.Fatalf("skipped[b] = %q, want already_settled", got.Skipped["b"])
	}
}

func TestTransitiveDependents_ReportsExternalWaits(t *testing.T) {
	// B is in the chain but also waits on Z, which nobody queued. Under a
	// paused project Z never runs, so the chain stalls at B — the single
	// most likely silent failure, hence it must be reported.
	b := feat("b", []string{"a", "z"}, "pending")
	b.WaitingOnFeatures = []string{"z"}
	fs := []*ComputedFeature{feat("a", nil, "pending"), b, feat("z", nil, "pending")}

	got := TransitiveDependents(fs, "a")
	if !reflect.DeepEqual(got.Members, []string{"b"}) {
		t.Fatalf("members = %v, want [b]", got.Members)
	}
	if !reflect.DeepEqual(got.External, []string{"z"}) {
		t.Fatalf("external = %v, want [z]", got.External)
	}
}

func TestTransitiveDependents_SettledExternalIsNotAnObstacle(t *testing.T) {
	b := feat("b", []string{"a", "z"}, "pending")
	b.WaitingOnFeatures = []string{"z"}
	z := feat("z", nil, "completed")
	got := TransitiveDependents([]*ComputedFeature{feat("a", nil, "pending"), b, z}, "a")
	if len(got.External) != 0 {
		t.Fatalf("external = %v, want none: a completed dependency blocks nothing", got.External)
	}
}

func TestTransitiveDependents_TerminatesOnCyclicGraph(t *testing.T) {
	// Guards against the reverse walk looping forever. Both are marked
	// InCycle as the real resolver would, so they are skipped, but the walk
	// itself must still terminate.
	b := feat("b", []string{"c"}, "pending")
	c := feat("c", []string{"b"}, "pending")
	b.InCycle, c.InCycle = true, true
	done := make(chan DependentClosure, 1)
	go func() {
		done <- TransitiveDependents([]*ComputedFeature{feat("a", nil, "pending"), b, c}, "a")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("TransitiveDependents did not terminate on a cyclic graph")
	}
}

func TestTransitiveDependents_TruncatesAndSaysSo(t *testing.T) {
	fs := []*ComputedFeature{feat("root", nil, "pending")}
	for i := 0; i < maxCascadeClosure+5; i++ {
		fs = append(fs, feat(fmt.Sprintf("f%02d", i), []string{"root"}, "pending"))
	}
	got := TransitiveDependents(fs, "root")
	if len(got.Members) != maxCascadeClosure {
		t.Fatalf("members = %d, want the cap %d", len(got.Members), maxCascadeClosure)
	}
	if !got.Truncated {
		t.Fatal("closure hit the cap without reporting Truncated; silent truncation reads as full coverage")
	}
}

func TestTransitiveDependents_EmptyInputs(t *testing.T) {
	if got := TransitiveDependents(nil, "a"); len(got.Members) != 0 {
		t.Fatalf("members = %v for a nil graph", got.Members)
	}
	if got := TransitiveDependents([]*ComputedFeature{feat("a", nil, "pending")}, ""); len(got.Members) != 0 {
		t.Fatalf("members = %v for an empty root", got.Members)
	}
}

// A settled feature classifies as "ready" — classifyFeature returns that for
// completed/archived with the comment "no classification needed". Anything
// deciding "is there work to dispatch here?" must therefore ALSO check for
// pending tasks, or it re-dispatches finished features forever.
//
// Pinned here because the trap lives in classifyFeature's contract, not in
// any one caller: the next caller to read Classification alone will hit it.
func TestClassifyFeature_CompletedIsReadyNotDispatchable(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Status: "completed", FeatureID: "done"},
	}
	features := ResolveFeatureDependencies(ComputeFeatures(tasks))
	if len(features) != 1 {
		t.Fatalf("features = %d, want 1", len(features))
	}
	f := features[0]
	if f.Status != "completed" {
		t.Fatalf("status = %q, want completed", f.Status)
	}
	if f.Classification != "ready" {
		t.Fatalf("classification = %q, want ready — if this changed, the "+
			"TaskStats.Pending guard in sweepProjectChains may no longer be needed",
			f.Classification)
	}
	if f.TaskStats.Pending != 0 {
		t.Fatalf("pending = %d, want 0 — this is the field that actually "+
			"distinguishes dispatchable from settled", f.TaskStats.Pending)
	}
}

// A settled feature must be TRAVERSED THROUGH, not treated as a wall.
//
// The BFS marks completed/archived features as skipped — correct, they need
// no dispatch — but it must still expand their dependents, because a settled
// feature's gate is OPEN and everything behind it is exactly the runnable
// work the request asked for.
//
// Getting this wrong is fatal rather than cosmetic: only the ROOT is
// persisted, so the closure is recomputed from scratch every sweep. A
// truncated closure IS the chain. In A <- B <- C, the moment B completes the
// closure collapses to empty, chainSettled sees only the root, and the chain
// retires at the exact instant C becomes dispatchable.
//
// A two-feature graph cannot catch this, which is why it survived the live
// demo — the collapse needs a third link.
func TestTransitiveDependents_TraversesThroughSettledFeatures(t *testing.T) {
	fs := []*ComputedFeature{
		feat("a", nil, "completed"),
		feat("b", []string{"a"}, "completed"),
		feat("c", []string{"b"}, "pending"),
		feat("d", []string{"c"}, "pending"),
	}
	got := TransitiveDependents(fs, "a")

	if !reflect.DeepEqual(got.Members, []string{"c", "d"}) {
		t.Fatalf("members = %v, want [c d]: a settled B must not hide C and D",
			got.Members)
	}
	if got.Skipped["b"] != "already_settled" {
		t.Fatalf("skipped[b] = %q, want already_settled (skipped, but still traversed)",
			got.Skipped["b"])
	}
}

// Members must never be nil: it is serialized as `queued` and consumed as an
// array by the PWA, where a null crashes the chain badge on `.length`.
func TestTransitiveDependents_MembersIsNeverNil(t *testing.T) {
	for _, tc := range []struct {
		name string
		fs   []*ComputedFeature
		root string
	}{
		{"no dependents", []*ComputedFeature{feat("a", nil, "pending")}, "a"},
		{"empty graph", nil, "a"},
		{"unknown root", []*ComputedFeature{feat("a", nil, "pending")}, "zz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TransitiveDependents(tc.fs, tc.root); got.Members == nil {
				t.Fatal("Members is nil; it marshals to `queued: null` and the PWA reads .length on it")
			}
		})
	}
}

// chainSettled must not retire a chain whose work is still in draft.
//
// computeTaskStats gives a draft task a Total slot and no bucket, so a check
// phrased as "Pending or InProgress > 0" reads it as finished. Retirement
// deletes the persisted root and there is no re-enrolment short of clicking
// again, so this loses the chain permanently.
func TestChainSettled_DraftWorkIsNotSettled(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "r1", Status: "completed", FeatureID: "root"},
		{ID: "d1", Status: "draft", FeatureID: "dep"},
	}
	features := ResolveFeatureDependencies(ComputeFeatures(tasks))
	byID := map[string]*ComputedFeature{}
	for _, f := range features {
		byID[f.ID] = f
	}

	// Guard the premise: draft really is unbucketed.
	dep := byID["dep"]
	if dep.TaskStats.Total != 1 || dep.TaskStats.Pending != 0 {
		t.Fatalf("premise changed: draft now buckets as %+v", dep.TaskStats)
	}

	s := &SchedulerService{}
	if s.chainSettled(byID, "root", []string{"dep"}) {
		t.Fatal("chain retired while a member's work was still in draft")
	}
}

func TestChainSettled_TerminalWorkIsSettled(t *testing.T) {
	// Blocked counts as terminal on purpose: the retry cap parks tasks there,
	// and waiting on one would keep the chain alive forever.
	tasks := []types.ResolvedTask{
		{ID: "r1", Status: "completed", FeatureID: "root"},
		{ID: "d1", Status: "blocked", FeatureID: "dep"},
	}
	features := ResolveFeatureDependencies(ComputeFeatures(tasks))
	byID := map[string]*ComputedFeature{}
	for _, f := range features {
		byID[f.ID] = f
	}
	s := &SchedulerService{}
	if !s.chainSettled(byID, "root", []string{"dep"}) {
		t.Fatal("chain stayed alive on work that cannot proceed without a human")
	}
}

func TestChainSettled_PendingWorkIsNotSettled(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "r1", Status: "completed", FeatureID: "root"},
		{ID: "d1", Status: "pending", FeatureID: "dep"},
	}
	features := ResolveFeatureDependencies(ComputeFeatures(tasks))
	byID := map[string]*ComputedFeature{}
	for _, f := range features {
		byID[f.ID] = f
	}
	s := &SchedulerService{}
	if s.chainSettled(byID, "root", []string{"dep"}) {
		t.Fatal("chain retired with pending work outstanding")
	}
}
