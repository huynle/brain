package service

import (
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// Regression coverage for feature_depends_on enforcement.
//
// The gate used to exist only in the report-only feature pipeline behind
// GET /features: ClassifyTask read task.DependsOn and nothing else, so a task
// in a feature whose dependency features had not finished still classified
// "ready", and both dispatch paths — the scheduler's push loop and GET /next —
// dispatched it. These tests pin the gate to ResolveDependencies, which is
// what every dispatch path reads.

// makeTaskWithFeature builds a task entry with feature grouping fields set.
//
// Restored here during the #34 + #36 merge: it lived in taskdeps_test.go and
// was removed with the GetNextTask tests that were its only other callers.
// These tests are now its sole consumer.
func makeTaskWithFeature(id, title, status, priority, featureID, featurePriority string, featureDeps []string, dependsOn []string) types.BrainEntry {
	e := makeTask(id, title, status, priority, dependsOn)
	e.FeatureID = featureID
	e.FeaturePriority = featurePriority
	e.FeatureDependsOn = featureDeps
	return e
}

// classificationOf finds a task in a resolved response by ID.
func classificationOf(t *testing.T, result *types.TaskListResponse, id string) types.ResolvedTask {
	t.Helper()
	for _, task := range result.Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q not found in resolved response", id)
	return types.ResolvedTask{}
}

func TestResolveDependencies_FeatureDepsGateReadyTask(t *testing.T) {
	// feat-b depends on feat-a, which is still pending. b1 has no task-level
	// deps at all, so the task layer alone calls it ready.
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "Fetch raw records", "pending", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("b1", "Build summary report", "pending", "high", "feat-b", "high", []string{"feat-a"}, nil),
	}

	result := ResolveDependencies(tasks)

	b1 := classificationOf(t, result, "b1")
	if b1.Classification != "waiting" {
		t.Errorf("b1.Classification = %q, want \"waiting\" (feat-b waits on feat-a)", b1.Classification)
	}
	if !reflect.DeepEqual(b1.WaitingOnFeatures, []string{"feat-a"}) {
		t.Errorf("b1.WaitingOnFeatures = %v, want [feat-a]", b1.WaitingOnFeatures)
	}
	if a1 := classificationOf(t, result, "a1"); a1.Classification != "ready" {
		t.Errorf("a1.Classification = %q, want \"ready\" (feat-a has no deps)", a1.Classification)
	}

	// The gate has to hold at the dispatch helper, not just on the struct:
	// GetReady/GetNext both funnel through GetReadyTasks.
	ready := GetReadyTasks(result)
	for _, task := range ready {
		if task.ID == "b1" {
			t.Fatal("GetReadyTasks returned b1: a feature-gated task is dispatchable")
		}
	}
	if len(ready) != 1 || ready[0].ID != "a1" {
		t.Errorf("GetReadyTasks returned %d tasks, want only a1", len(ready))
	}

	// Stats must describe what will actually dispatch.
	if result.Stats.Ready != 1 || result.Stats.Waiting != 1 {
		t.Errorf("stats ready=%d waiting=%d, want ready=1 waiting=1", result.Stats.Ready, result.Stats.Waiting)
	}
}

func TestResolveDependencies_FeatureDepsReleaseWhenDepCompletes(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "Fetch raw records", "completed", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("b1", "Build summary report", "pending", "high", "feat-b", "high", []string{"feat-a"}, nil),
	}

	result := ResolveDependencies(tasks)

	b1 := classificationOf(t, result, "b1")
	if b1.Classification != "ready" {
		t.Errorf("b1.Classification = %q, want \"ready\" (feat-a completed)", b1.Classification)
	}
	if len(b1.WaitingOnFeatures) != 0 {
		t.Errorf("b1.WaitingOnFeatures = %v, want empty", b1.WaitingOnFeatures)
	}
}

func TestResolveDependencies_FeatureDepsBlockedByCancelledDep(t *testing.T) {
	// A cancelled task makes feat-a "blocked", which hard-blocks downstream
	// features — the same convention ClassifyTask uses for cancelled task
	// deps at the task level.
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "Fetch raw records", "cancelled", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("b1", "Build summary report", "pending", "high", "feat-b", "high", []string{"feat-a"}, nil),
	}

	result := ResolveDependencies(tasks)

	b1 := classificationOf(t, result, "b1")
	if b1.Classification != "blocked" {
		t.Errorf("b1.Classification = %q, want \"blocked\"", b1.Classification)
	}
	if b1.BlockedByReason != "feature_dependency_blocked" {
		t.Errorf("b1.BlockedByReason = %q, want \"feature_dependency_blocked\"", b1.BlockedByReason)
	}
	if !reflect.DeepEqual(b1.BlockedByFeatures, []string{"feat-a"}) {
		t.Errorf("b1.BlockedByFeatures = %v, want [feat-a]", b1.BlockedByFeatures)
	}
}

func TestResolveDependencies_FeatureDepsCycleBlocks(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "A", "pending", "high", "feat-a", "high", []string{"feat-b"}, nil),
		makeTaskWithFeature("b1", "B", "pending", "high", "feat-b", "high", []string{"feat-a"}, nil),
	}

	result := ResolveDependencies(tasks)

	for _, id := range []string{"a1", "b1"} {
		task := classificationOf(t, result, id)
		if task.Classification != "blocked" {
			t.Errorf("%s.Classification = %q, want \"blocked\" (feature cycle)", id, task.Classification)
		}
		if task.BlockedByReason != "feature_circular_dependency" {
			t.Errorf("%s.BlockedByReason = %q, want \"feature_circular_dependency\"", id, task.BlockedByReason)
		}
	}
	if len(GetReadyTasks(result)) != 0 {
		t.Error("GetReadyTasks returned tasks from a feature dependency cycle")
	}
}

func TestResolveDependencies_UnresolvedFeatureDepIsReportedNotEnforced(t *testing.T) {
	// A misspelled feature_depends_on used to be dropped silently: it gated
	// nothing AND reported nothing. It still does not gate — same convention
	// as UnresolvedDeps at the task level, and blocking on a feature that may
	// not exist yet would deadlock — but it is now visible.
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "A", "pending", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("b1", "B", "pending", "high", "feat-b", "high", []string{"feat-typo"}, nil),
	}

	result := ResolveDependencies(tasks)

	b1 := classificationOf(t, result, "b1")
	if b1.Classification != "ready" {
		t.Errorf("b1.Classification = %q, want \"ready\" (unresolved deps do not gate)", b1.Classification)
	}
	if !reflect.DeepEqual(b1.UnresolvedFeatureDeps, []string{"feat-typo"}) {
		t.Errorf("b1.UnresolvedFeatureDeps = %v, want [feat-typo]", b1.UnresolvedFeatureDeps)
	}
	if a1 := classificationOf(t, result, "a1"); len(a1.UnresolvedFeatureDeps) != 0 {
		t.Errorf("a1.UnresolvedFeatureDeps = %v, want empty", a1.UnresolvedFeatureDeps)
	}
}

func TestResolveDependencies_UnresolvedFeatureDepReportedOnGatedTask(t *testing.T) {
	// One good dep and one typo on the same feature: the good dep gates, and
	// the typo is still reported on the (now waiting) task rather than being
	// swallowed by the early-out.
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "A", "pending", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("b1", "B", "pending", "high", "feat-b", "high", []string{"feat-a", "feat-typo"}, nil),
	}

	result := ResolveDependencies(tasks)

	b1 := classificationOf(t, result, "b1")
	if b1.Classification != "waiting" {
		t.Errorf("b1.Classification = %q, want \"waiting\"", b1.Classification)
	}
	if !reflect.DeepEqual(b1.UnresolvedFeatureDeps, []string{"feat-typo"}) {
		t.Errorf("b1.UnresolvedFeatureDeps = %v, want [feat-typo]", b1.UnresolvedFeatureDeps)
	}
	if !reflect.DeepEqual(b1.WaitingOnFeatures, []string{"feat-a"}) {
		t.Errorf("b1.WaitingOnFeatures = %v, want [feat-a]", b1.WaitingOnFeatures)
	}
}

func TestResolveDependencies_FeatureGatingPreservesTaskLevelReason(t *testing.T) {
	// b1 is blocked by a cancelled TASK dep and also sits in a gated feature.
	// The task-level reason is the more specific one and must survive.
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "A", "pending", "high", "feat-a", "high", nil, nil),
		makeTask("x1", "Cancelled dep", "cancelled", "high", nil),
		makeTaskWithFeature("b1", "B", "pending", "high", "feat-b", "high", []string{"feat-a"}, []string{"x1"}),
	}

	result := ResolveDependencies(tasks)

	b1 := classificationOf(t, result, "b1")
	if b1.Classification != "blocked" {
		t.Errorf("b1.Classification = %q, want \"blocked\"", b1.Classification)
	}
	if b1.BlockedByReason != "dependency_blocked" {
		t.Errorf("b1.BlockedByReason = %q, want task-level \"dependency_blocked\"", b1.BlockedByReason)
	}
	if !reflect.DeepEqual(b1.BlockedBy, []string{"x1"}) {
		t.Errorf("b1.BlockedBy = %v, want [x1]", b1.BlockedBy)
	}
	if len(b1.BlockedByFeatures) != 0 {
		t.Errorf("b1.BlockedByFeatures = %v, want empty (task-level reason wins)", b1.BlockedByFeatures)
	}
}

func TestResolveDependencies_UngroupedTasksUnaffected(t *testing.T) {
	// Tasks with no feature_id must never be touched by the gate, even when
	// other features in the project are gated.
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "A", "pending", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("b1", "B", "pending", "high", "feat-b", "high", []string{"feat-a"}, nil),
		makeTask("u1", "Ungrouped", "pending", "low", nil),
	}

	result := ResolveDependencies(tasks)

	u1 := classificationOf(t, result, "u1")
	if u1.Classification != "ready" {
		t.Errorf("u1.Classification = %q, want \"ready\"", u1.Classification)
	}
	if u1.WaitingOnFeatures != nil || u1.BlockedByFeatures != nil || u1.UnresolvedFeatureDeps != nil {
		t.Error("ungrouped task carries feature gating fields")
	}
}

func TestResolveDependencies_FeatureGatingIsChained(t *testing.T) {
	// a -> b -> c. Only feat-a may run; the two downstream features wait,
	// including the one whose dependency is itself waiting rather than
	// pending-with-no-deps.
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "A", "pending", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("b1", "B", "pending", "high", "feat-b", "high", []string{"feat-a"}, nil),
		makeTaskWithFeature("c1", "C", "pending", "high", "feat-c", "high", []string{"feat-b"}, nil),
	}

	result := ResolveDependencies(tasks)

	ready := GetReadyTasks(result)
	if len(ready) != 1 || ready[0].ID != "a1" {
		ids := make([]string, len(ready))
		for i, task := range ready {
			ids[i] = task.ID
		}
		t.Errorf("ready = %v, want only [a1]", ids)
	}
}

func TestResolveDependencies_ArchivedDependencyFeatureDoesNotGate(t *testing.T) {
	// An all-archived feature is settled, not in-flight: it must not hold
	// downstream work hostage forever.
	tasks := []types.BrainEntry{
		makeTaskWithFeature("a1", "A", "archived", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("b1", "B", "pending", "high", "feat-b", "high", []string{"feat-a"}, nil),
	}

	result := ResolveDependencies(tasks)

	if b1 := classificationOf(t, result, "b1"); b1.Classification != "ready" {
		t.Errorf("b1.Classification = %q, want \"ready\" (feat-a archived)", b1.Classification)
	}
}

func TestComputeFeatures_DependencyOrderIsStable(t *testing.T) {
	// collectFeatureDependencies used to range over a map, so these IDs —
	// which are serialized to clients via waiting_on/unresolved lists —
	// shuffled between identical requests.
	tasks := []types.ResolvedTask{
		{ID: "b1", Path: "projects/test/task/b1.md", Status: "pending", FeatureID: "feat-b",
			FeatureDependsOn: []string{"feat-a", "feat-c", "feat-d", "feat-e", "feat-f"}},
	}

	want := []string{"feat-a", "feat-c", "feat-d", "feat-e", "feat-f"}
	for i := 0; i < 20; i++ {
		features := ComputeFeatures(tasks)
		if !reflect.DeepEqual(features[0].DependsOnFeatures, want) {
			t.Fatalf("iteration %d: DependsOnFeatures = %v, want %v",
				i, features[0].DependsOnFeatures, want)
		}
	}
}

func TestResolveFeatureDependencies_ReportsUnresolvedDeps(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "a1", Path: "projects/test/task/a1.md", Status: "pending", FeatureID: "feat-a"},
		{ID: "b1", Path: "projects/test/task/b1.md", Status: "pending", FeatureID: "feat-b",
			FeatureDependsOn: []string{"feat-a", "feat-ghost"}},
	}

	resolved := ResolveFeatureDependencies(ComputeFeatures(tasks))

	var featB *ComputedFeature
	for _, f := range resolved {
		if f.ID == "feat-b" {
			featB = f
		}
	}
	if featB == nil {
		t.Fatal("feat-b missing from resolution")
	}
	if !reflect.DeepEqual(featB.UnresolvedFeatureDeps, []string{"feat-ghost"}) {
		t.Errorf("UnresolvedFeatureDeps = %v, want [feat-ghost]", featB.UnresolvedFeatureDeps)
	}
	if !reflect.DeepEqual(featB.WaitingOnFeatures, []string{"feat-a"}) {
		t.Errorf("WaitingOnFeatures = %v, want [feat-a]", featB.WaitingOnFeatures)
	}
}
