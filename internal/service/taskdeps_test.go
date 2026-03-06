package service

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Helper: make a minimal BrainEntry for testing
// ---------------------------------------------------------------------------

func makeTask(id, title, status, priority string, dependsOn []string) types.BrainEntry {
	return types.BrainEntry{
		ID:        id,
		Path:      "projects/test/task/" + id + ".md",
		Title:     title,
		Type:      "task",
		Status:    status,
		Priority:  priority,
		DependsOn: dependsOn,
		Created:   "2025-01-01T00:00:00Z",
	}
}

func makeTaskWithCreated(id, title, status, priority, created string, dependsOn []string) types.BrainEntry {
	e := makeTask(id, title, status, priority, dependsOn)
	e.Created = created
	return e
}

func makeTaskWithFeature(id, title, status, priority, featureID, featurePriority string, featureDeps []string, dependsOn []string) types.BrainEntry {
	e := makeTask(id, title, status, priority, dependsOn)
	e.FeatureID = featureID
	e.FeaturePriority = featurePriority
	e.FeatureDependsOn = featureDeps
	return e
}

// ---------------------------------------------------------------------------
// BuildLookupMaps
// ---------------------------------------------------------------------------

func TestBuildLookupMaps(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("aaa", "Task A", "pending", "high", nil),
		makeTask("bbb", "Task B", "pending", "medium", nil),
	}

	maps := BuildLookupMaps(tasks)

	if _, ok := maps.ByID["aaa"]; !ok {
		t.Error("expected ByID to contain 'aaa'")
	}
	if _, ok := maps.ByID["bbb"]; !ok {
		t.Error("expected ByID to contain 'bbb'")
	}
	if id, ok := maps.TitleToID["Task A"]; !ok || id != "aaa" {
		t.Errorf("TitleToID['Task A'] = %q, want 'aaa'", id)
	}
	if id, ok := maps.TitleToID["Task B"]; !ok || id != "bbb" {
		t.Errorf("TitleToID['Task B'] = %q, want 'bbb'", id)
	}
}

func TestBuildLookupMaps_Empty(t *testing.T) {
	maps := BuildLookupMaps(nil)
	if len(maps.ByID) != 0 {
		t.Errorf("expected empty ByID, got %d entries", len(maps.ByID))
	}
	if len(maps.TitleToID) != 0 {
		t.Errorf("expected empty TitleToID, got %d entries", len(maps.TitleToID))
	}
}

// ---------------------------------------------------------------------------
// ResolveDep
// ---------------------------------------------------------------------------

func TestResolveDep(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("abc12def", "My Task", "pending", "high", nil),
		makeTask("xyz98765", "Other Task", "pending", "medium", nil),
	}
	maps := BuildLookupMaps(tasks)

	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"direct ID", "abc12def", "abc12def"},
		{"colon syntax", "brain-api:abc12def", "abc12def"},
		{"full path", "projects/brain-api/task/abc12def.md", "abc12def"},
		{"full path without .md", "projects/brain-api/task/abc12def", "abc12def"},
		{"title match", "My Task", "abc12def"},
		{"unknown ref", "nonexistent", ""},
		{"unknown colon ref", "project:nonexistent", ""},
		{"unknown path", "projects/x/task/nonexistent.md", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDep(tt.ref, maps)
			if got != tt.want {
				t.Errorf("ResolveDep(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BuildAdjacencyList
// ---------------------------------------------------------------------------

func TestBuildAdjacencyList(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", nil),
		makeTask("b", "Task B", "pending", "medium", []string{"a"}),
		makeTask("c", "Task C", "pending", "low", []string{"a", "b"}),
	}
	maps := BuildLookupMaps(tasks)
	adj := BuildAdjacencyList(tasks, maps)

	if deps := adj["a"]; len(deps) != 0 {
		t.Errorf("a deps = %v, want empty", deps)
	}
	if deps := adj["b"]; len(deps) != 1 || deps[0] != "a" {
		t.Errorf("b deps = %v, want [a]", deps)
	}
	if deps := adj["c"]; len(deps) != 2 {
		t.Errorf("c deps = %v, want [a, b]", deps)
	}
}

func TestBuildAdjacencyList_UnresolvedDepsSkipped(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", []string{"nonexistent"}),
	}
	maps := BuildLookupMaps(tasks)
	adj := BuildAdjacencyList(tasks, maps)

	if deps := adj["a"]; len(deps) != 0 {
		t.Errorf("a deps = %v, want empty (unresolved should be skipped)", deps)
	}
}

// ---------------------------------------------------------------------------
// FindCycles
// ---------------------------------------------------------------------------

func TestFindCycles_NoCycles(t *testing.T) {
	adj := map[string][]string{
		"a": {},
		"b": {"a"},
		"c": {"b"},
	}
	cycles := FindCycles(adj)
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %v", cycles)
	}
}

func TestFindCycles_SimpleCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	cycles := FindCycles(adj)
	if !cycles["a"] {
		t.Error("expected 'a' to be in cycle")
	}
	if !cycles["b"] {
		t.Error("expected 'b' to be in cycle")
	}
}

func TestFindCycles_ThreeNodeCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}
	cycles := FindCycles(adj)
	for _, id := range []string{"a", "b", "c"} {
		if !cycles[id] {
			t.Errorf("expected %q to be in cycle", id)
		}
	}
}

func TestFindCycles_PartialCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"a"},
		"c": {},
		"d": {"a"},
	}
	cycles := FindCycles(adj)
	if !cycles["a"] {
		t.Error("expected 'a' in cycle")
	}
	if !cycles["b"] {
		t.Error("expected 'b' in cycle")
	}
	if cycles["d"] {
		t.Error("expected 'd' NOT in cycle")
	}
}

func TestFindCycles_SelfLoop(t *testing.T) {
	adj := map[string][]string{
		"a": {"a"},
	}
	cycles := FindCycles(adj)
	if !cycles["a"] {
		t.Error("expected 'a' to be in cycle (self-loop)")
	}
}

// ---------------------------------------------------------------------------
// ClassifyTask
// ---------------------------------------------------------------------------

func TestClassifyTask(t *testing.T) {
	effectiveStatuses := map[string]string{
		"dep1": "completed",
		"dep2": "pending",
		"dep3": "blocked",
		"dep4": "validated",
		"dep5": "in_progress",
		"dep6": "cancelled",
	}

	tests := []struct {
		name           string
		task           types.BrainEntry
		resolvedDeps   []string
		inCycle        bool
		wantClass      string
		wantBlockedLen int
		wantWaitingLen int
	}{
		{
			name:      "in cycle",
			task:      makeTask("x", "X", "pending", "high", nil),
			inCycle:   true,
			wantClass: "blocked",
		},
		{
			name:      "not pending",
			task:      makeTask("x", "X", "completed", "high", nil),
			wantClass: "not_pending",
		},
		{
			name:         "all deps completed",
			task:         makeTask("x", "X", "pending", "high", nil),
			resolvedDeps: []string{"dep1", "dep4"},
			wantClass:    "ready",
		},
		{
			name:         "no deps",
			task:         makeTask("x", "X", "pending", "high", nil),
			resolvedDeps: nil,
			wantClass:    "ready",
		},
		{
			name:           "dep blocked",
			task:           makeTask("x", "X", "pending", "high", nil),
			resolvedDeps:   []string{"dep3"},
			wantClass:      "blocked",
			wantBlockedLen: 1,
		},
		{
			name:           "dep cancelled",
			task:           makeTask("x", "X", "pending", "high", nil),
			resolvedDeps:   []string{"dep6"},
			wantClass:      "blocked",
			wantBlockedLen: 1,
		},
		{
			name:           "dep pending (waiting)",
			task:           makeTask("x", "X", "pending", "high", nil),
			resolvedDeps:   []string{"dep2"},
			wantClass:      "waiting",
			wantWaitingLen: 1,
		},
		{
			name:           "dep in_progress (waiting)",
			task:           makeTask("x", "X", "pending", "high", nil),
			resolvedDeps:   []string{"dep5"},
			wantClass:      "waiting",
			wantWaitingLen: 1,
		},
		{
			name:           "blocked takes precedence over waiting",
			task:           makeTask("x", "X", "pending", "high", nil),
			resolvedDeps:   []string{"dep2", "dep3"},
			wantClass:      "blocked",
			wantBlockedLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inCycleSet := map[string]bool{}
			if tt.inCycle {
				inCycleSet[tt.task.ID] = true
			}
			classification, blockedBy, waitingOn := ClassifyTask(&tt.task, tt.resolvedDeps, effectiveStatuses, inCycleSet)
			if classification != tt.wantClass {
				t.Errorf("classification = %q, want %q", classification, tt.wantClass)
			}
			if tt.wantBlockedLen > 0 && len(blockedBy) != tt.wantBlockedLen {
				t.Errorf("blockedBy len = %d, want %d", len(blockedBy), tt.wantBlockedLen)
			}
			if tt.wantWaitingLen > 0 && len(waitingOn) != tt.wantWaitingLen {
				t.Errorf("waitingOn len = %d, want %d", len(waitingOn), tt.wantWaitingLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ResolveDependencies (main entry point)
// ---------------------------------------------------------------------------

func TestResolveDependencies_Empty(t *testing.T) {
	result := ResolveDependencies(nil)
	if len(result.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(result.Tasks))
	}
	if result.Stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if result.Stats.Total != 0 {
		t.Errorf("expected total=0, got %d", result.Stats.Total)
	}
}

func TestResolveDependencies_NoDeps(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", nil),
		makeTask("b", "Task B", "pending", "medium", nil),
	}
	result := ResolveDependencies(tasks)

	if len(result.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result.Tasks))
	}
	if result.Stats.Total != 2 {
		t.Errorf("stats.total = %d, want 2", result.Stats.Total)
	}
	if result.Stats.Ready != 2 {
		t.Errorf("stats.ready = %d, want 2", result.Stats.Ready)
	}
	for _, task := range result.Tasks {
		if task.Classification != "ready" {
			t.Errorf("task %q classification = %q, want 'ready'", task.ID, task.Classification)
		}
	}
}

func TestResolveDependencies_LinearChain(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "completed", "high", nil),
		makeTask("b", "Task B", "pending", "medium", []string{"a"}),
		makeTask("c", "Task C", "pending", "low", []string{"b"}),
	}
	result := ResolveDependencies(tasks)

	taskMap := make(map[string]types.ResolvedTask)
	for _, rt := range result.Tasks {
		taskMap[rt.ID] = rt
	}

	if taskMap["a"].Classification != "not_pending" {
		t.Errorf("a classification = %q, want 'not_pending'", taskMap["a"].Classification)
	}
	if taskMap["b"].Classification != "ready" {
		t.Errorf("b classification = %q, want 'ready'", taskMap["b"].Classification)
	}
	if taskMap["c"].Classification != "waiting" {
		t.Errorf("c classification = %q, want 'waiting'", taskMap["c"].Classification)
	}
}

func TestResolveDependencies_WithCycle(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", []string{"b"}),
		makeTask("b", "Task B", "pending", "medium", []string{"a"}),
		makeTask("c", "Task C", "pending", "low", nil),
	}
	result := ResolveDependencies(tasks)

	taskMap := make(map[string]types.ResolvedTask)
	for _, rt := range result.Tasks {
		taskMap[rt.ID] = rt
	}

	if taskMap["a"].Classification != "blocked" {
		t.Errorf("a classification = %q, want 'blocked'", taskMap["a"].Classification)
	}
	if !taskMap["a"].InCycle {
		t.Error("expected a to be in cycle")
	}
	if taskMap["b"].Classification != "blocked" {
		t.Errorf("b classification = %q, want 'blocked'", taskMap["b"].Classification)
	}
	if !taskMap["b"].InCycle {
		t.Error("expected b to be in cycle")
	}
	if taskMap["c"].Classification != "ready" {
		t.Errorf("c classification = %q, want 'ready'", taskMap["c"].Classification)
	}
	if len(result.Cycles) == 0 {
		t.Error("expected cycles to be reported")
	}
}

func TestResolveDependencies_UnresolvedDeps(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", []string{"nonexistent"}),
	}
	result := ResolveDependencies(tasks)

	if len(result.Tasks[0].UnresolvedDeps) != 1 {
		t.Errorf("expected 1 unresolved dep, got %d", len(result.Tasks[0].UnresolvedDeps))
	}
	if result.Tasks[0].UnresolvedDeps[0] != "nonexistent" {
		t.Errorf("unresolved dep = %q, want 'nonexistent'", result.Tasks[0].UnresolvedDeps[0])
	}
	if result.Tasks[0].Classification != "ready" {
		t.Errorf("classification = %q, want 'ready'", result.Tasks[0].Classification)
	}
}

func TestResolveDependencies_Stats(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "completed", "high", nil),
		makeTask("b", "Task B", "pending", "medium", nil),
		makeTask("c", "Task C", "pending", "low", []string{"b"}),
		makeTask("d", "Task D", "pending", "high", []string{"e"}),
		makeTask("e", "Task E", "pending", "high", []string{"d"}),
	}
	result := ResolveDependencies(tasks)

	if result.Stats.Total != 5 {
		t.Errorf("total = %d, want 5", result.Stats.Total)
	}
	if result.Stats.NotPending != 1 {
		t.Errorf("not_pending = %d, want 1", result.Stats.NotPending)
	}
	if result.Stats.Ready != 1 {
		t.Errorf("ready = %d, want 1", result.Stats.Ready)
	}
	if result.Stats.Waiting != 1 {
		t.Errorf("waiting = %d, want 1", result.Stats.Waiting)
	}
	if result.Stats.Blocked != 2 {
		t.Errorf("blocked = %d, want 2", result.Stats.Blocked)
	}
}

func TestResolveDependencies_DepOnBlockedTask(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "blocked", "high", nil),
		makeTask("b", "Task B", "pending", "medium", []string{"a"}),
	}
	result := ResolveDependencies(tasks)

	taskMap := make(map[string]types.ResolvedTask)
	for _, rt := range result.Tasks {
		taskMap[rt.ID] = rt
	}

	if taskMap["b"].Classification != "blocked" {
		t.Errorf("b classification = %q, want 'blocked'", taskMap["b"].Classification)
	}
	if len(taskMap["b"].BlockedBy) != 1 || taskMap["b"].BlockedBy[0] != "a" {
		t.Errorf("b blocked_by = %v, want [a]", taskMap["b"].BlockedBy)
	}
}

func TestResolveDependencies_SatisfiedStatuses(t *testing.T) {
	satisfiedStatuses := []string{"completed", "validated", "superseded", "archived"}
	for _, status := range satisfiedStatuses {
		t.Run(status, func(t *testing.T) {
			tasks := []types.BrainEntry{
				makeTask("dep", "Dep", status, "high", nil),
				makeTask("task", "Task", "pending", "medium", []string{"dep"}),
			}
			result := ResolveDependencies(tasks)
			taskMap := make(map[string]types.ResolvedTask)
			for _, rt := range result.Tasks {
				taskMap[rt.ID] = rt
			}
			if taskMap["task"].Classification != "ready" {
				t.Errorf("with dep status %q, task classification = %q, want 'ready'", status, taskMap["task"].Classification)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SortByPriority
// ---------------------------------------------------------------------------

func TestSortByPriority(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "low1", Priority: "low", Created: "2025-01-01T00:00:00Z"},
		{ID: "high1", Priority: "high", Created: "2025-01-01T00:00:00Z"},
		{ID: "med1", Priority: "medium", Created: "2025-01-01T00:00:00Z"},
		{ID: "none1", Priority: "", Created: "2025-01-01T00:00:00Z"},
	}

	sorted := SortByPriority(tasks)

	wantOrder := []string{"high1", "med1", "low1", "none1"}
	for i, want := range wantOrder {
		if sorted[i].ID != want {
			t.Errorf("sorted[%d].ID = %q, want %q", i, sorted[i].ID, want)
		}
	}
}

func TestSortByPriority_SecondaryByCreated(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "b", Priority: "high", Created: "2025-01-02T00:00:00Z"},
		{ID: "a", Priority: "high", Created: "2025-01-01T00:00:00Z"},
	}

	sorted := SortByPriority(tasks)

	if sorted[0].ID != "a" {
		t.Errorf("sorted[0].ID = %q, want 'a' (earlier created)", sorted[0].ID)
	}
	if sorted[1].ID != "b" {
		t.Errorf("sorted[1].ID = %q, want 'b'", sorted[1].ID)
	}
}

func TestSortByPriority_DoesNotMutateOriginal(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "low1", Priority: "low"},
		{ID: "high1", Priority: "high"},
	}

	_ = SortByPriority(tasks)

	if tasks[0].ID != "low1" {
		t.Error("SortByPriority mutated the original slice")
	}
}

// ---------------------------------------------------------------------------
// GetReadyTasks / GetWaitingTasks / GetBlockedTasks
// ---------------------------------------------------------------------------

func TestGetReadyTasks(t *testing.T) {
	result := &types.TaskListResponse{
		Tasks: []types.ResolvedTask{
			{ID: "a", Classification: "ready", Priority: "low", Created: "2025-01-01T00:00:00Z"},
			{ID: "b", Classification: "waiting", Priority: "high", Created: "2025-01-01T00:00:00Z"},
			{ID: "c", Classification: "ready", Priority: "high", Created: "2025-01-01T00:00:00Z"},
		},
	}

	ready := GetReadyTasks(result)
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready tasks, got %d", len(ready))
	}
	if ready[0].ID != "c" {
		t.Errorf("ready[0].ID = %q, want 'c' (high priority)", ready[0].ID)
	}
	if ready[1].ID != "a" {
		t.Errorf("ready[1].ID = %q, want 'a' (low priority)", ready[1].ID)
	}
}

func TestGetWaitingTasks(t *testing.T) {
	result := &types.TaskListResponse{
		Tasks: []types.ResolvedTask{
			{ID: "a", Classification: "ready"},
			{ID: "b", Classification: "waiting"},
			{ID: "c", Classification: "blocked"},
		},
	}

	waiting := GetWaitingTasks(result)
	if len(waiting) != 1 || waiting[0].ID != "b" {
		t.Errorf("waiting = %v, want [b]", waiting)
	}
}

func TestGetBlockedTasks(t *testing.T) {
	result := &types.TaskListResponse{
		Tasks: []types.ResolvedTask{
			{ID: "a", Classification: "ready"},
			{ID: "b", Classification: "blocked"},
		},
	}

	blocked := GetBlockedTasks(result)
	if len(blocked) != 1 || blocked[0].ID != "b" {
		t.Errorf("blocked = %v, want [b]", blocked)
	}
}

// ---------------------------------------------------------------------------
// GetNextTask (feature-aware)
// ---------------------------------------------------------------------------

func TestGetNextTask_NoTasks(t *testing.T) {
	result := &types.TaskListResponse{Tasks: nil}
	next := GetNextTask(result)
	if next != nil {
		t.Errorf("expected nil, got %v", next)
	}
}

func TestGetNextTask_NoFeatures(t *testing.T) {
	result := &types.TaskListResponse{
		Tasks: []types.ResolvedTask{
			{ID: "a", Classification: "ready", Priority: "low", Created: "2025-01-02T00:00:00Z"},
			{ID: "b", Classification: "ready", Priority: "high", Created: "2025-01-01T00:00:00Z"},
		},
	}

	next := GetNextTask(result)
	if next == nil {
		t.Fatal("expected non-nil task")
	}
	if next.ID != "b" {
		t.Errorf("next.ID = %q, want 'b' (highest priority)", next.ID)
	}
}

func TestGetNextTask_WithFeatures(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTaskWithFeature("t1", "Task 1", "pending", "low", "feat-a", "high", nil, nil),
		makeTaskWithFeature("t2", "Task 2", "pending", "high", "feat-b", "low", nil, nil),
	}
	result := ResolveDependencies(tasks)

	next := GetNextTask(result)
	if next == nil {
		t.Fatal("expected non-nil task")
	}
	if next.ID != "t1" {
		t.Errorf("next.ID = %q, want 't1' (from higher priority feature)", next.ID)
	}
}

func TestGetNextTask_FallbackToUngrouped(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTaskWithFeature("t1", "Task 1", "pending", "high", "feat-a", "high", nil, []string{"t2"}),
		makeTask("t2", "Task 2", "pending", "medium", nil),
	}
	result := ResolveDependencies(tasks)

	next := GetNextTask(result)
	if next == nil {
		t.Fatal("expected non-nil task")
	}
	if next.ID != "t2" {
		t.Errorf("next.ID = %q, want 't2' (ungrouped fallback)", next.ID)
	}
}

func TestGetNextTask_FeatureDepsBlocked(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTaskWithFeature("t1", "Task 1", "pending", "high", "feat-a", "high", nil, nil),
		makeTaskWithFeature("t2", "Task 2", "pending", "high", "feat-b", "high", []string{"feat-a"}, nil),
		makeTask("t3", "Ungrouped", "pending", "low", nil),
	}
	result := ResolveDependencies(tasks)

	next := GetNextTask(result)
	if next == nil {
		t.Fatal("expected non-nil task")
	}
	if next.ID != "t1" {
		t.Errorf("next.ID = %q, want 't1' (from ready feature feat-a)", next.ID)
	}
}

// ---------------------------------------------------------------------------
// GetDownstreamTasks
// ---------------------------------------------------------------------------

func TestGetDownstreamTasks_NoTasks(t *testing.T) {
	result := GetDownstreamTasks("a", nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d tasks", len(result))
	}
}

func TestGetDownstreamTasks_RootNotFound(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", nil),
	}
	result := GetDownstreamTasks("nonexistent", tasks)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d tasks", len(result))
	}
}

func TestGetDownstreamTasks_RootOnly(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", nil),
		makeTask("b", "Task B", "pending", "medium", nil),
	}
	result := GetDownstreamTasks("a", tasks)
	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	if result[0].ID != "a" {
		t.Errorf("result[0].ID = %q, want 'a'", result[0].ID)
	}
}

func TestGetDownstreamTasks_LinearChain(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", nil),
		makeTask("b", "Task B", "pending", "medium", []string{"a"}),
		makeTask("c", "Task C", "pending", "low", []string{"b"}),
	}
	result := GetDownstreamTasks("a", tasks)

	if len(result) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(result))
	}
	if result[0].ID != "a" {
		t.Errorf("result[0].ID = %q, want 'a'", result[0].ID)
	}
	if result[1].ID != "b" {
		t.Errorf("result[1].ID = %q, want 'b'", result[1].ID)
	}
	if result[2].ID != "c" {
		t.Errorf("result[2].ID = %q, want 'c'", result[2].ID)
	}
}

func TestGetDownstreamTasks_Diamond(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", nil),
		makeTask("b", "Task B", "pending", "medium", []string{"a"}),
		makeTask("c", "Task C", "pending", "medium", []string{"a"}),
		makeTask("d", "Task D", "pending", "low", []string{"b", "c"}),
	}
	result := GetDownstreamTasks("a", tasks)

	if len(result) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(result))
	}
	if result[0].ID != "a" {
		t.Errorf("result[0].ID = %q, want 'a'", result[0].ID)
	}
	if result[len(result)-1].ID != "d" {
		t.Errorf("last task ID = %q, want 'd'", result[len(result)-1].ID)
	}
}

func TestGetDownstreamTasks_WithCycle(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "Task A", "pending", "high", nil),
		makeTask("b", "Task B", "pending", "medium", []string{"a", "c"}),
		makeTask("c", "Task C", "pending", "low", []string{"b"}),
	}

	result := GetDownstreamTasks("a", tasks)

	if len(result) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(result))
	}
	if result[0].ID != "a" {
		t.Errorf("result[0].ID = %q, want 'a'", result[0].ID)
	}
}

// ---------------------------------------------------------------------------
// ResolveDependencies: ResolvedTask field mapping
// ---------------------------------------------------------------------------

func TestResolveDependencies_FieldMapping(t *testing.T) {
	task := makeTask("abc", "My Task", "pending", "high", nil)
	task.FeatureID = "feat-1"
	task.FeaturePriority = "medium"
	task.FeatureDependsOn = []string{"feat-0"}
	task.DirectPrompt = "do something"
	task.Agent = "dev"
	task.Model = "claude"
	task.Workdir = "/tmp"
	task.GitRemote = "origin"
	task.GitBranch = "main"

	result := ResolveDependencies([]types.BrainEntry{task})
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	rt := result.Tasks[0]
	if rt.ID != "abc" {
		t.Errorf("ID = %q, want 'abc'", rt.ID)
	}
	if rt.Title != "My Task" {
		t.Errorf("Title = %q, want 'My Task'", rt.Title)
	}
	if rt.Status != "pending" {
		t.Errorf("Status = %q, want 'pending'", rt.Status)
	}
	if rt.Priority != "high" {
		t.Errorf("Priority = %q, want 'high'", rt.Priority)
	}
	if rt.FeatureID != "feat-1" {
		t.Errorf("FeatureID = %q, want 'feat-1'", rt.FeatureID)
	}
	if rt.FeaturePriority != "medium" {
		t.Errorf("FeaturePriority = %q, want 'medium'", rt.FeaturePriority)
	}
	if rt.DirectPrompt != "do something" {
		t.Errorf("DirectPrompt = %q, want 'do something'", rt.DirectPrompt)
	}
	if rt.Agent != "dev" {
		t.Errorf("Agent = %q, want 'dev'", rt.Agent)
	}
	if rt.Model != "claude" {
		t.Errorf("Model = %q, want 'claude'", rt.Model)
	}
	if rt.Workdir != "/tmp" {
		t.Errorf("Workdir = %q, want '/tmp'", rt.Workdir)
	}
	if rt.GitRemote != "origin" {
		t.Errorf("GitRemote = %q, want 'origin'", rt.GitRemote)
	}
	if rt.GitBranch != "main" {
		t.Errorf("GitBranch = %q, want 'main'", rt.GitBranch)
	}
	if rt.Classification != "ready" {
		t.Errorf("Classification = %q, want 'ready'", rt.Classification)
	}
}

// ---------------------------------------------------------------------------
// Edge case: dep on task in cycle should block dependent
// ---------------------------------------------------------------------------

func TestResolveDependencies_DepOnCycleParticipant(t *testing.T) {
	tasks := []types.BrainEntry{
		makeTask("a", "A", "pending", "high", []string{"b"}),
		makeTask("b", "B", "pending", "high", []string{"a"}),
		makeTask("c", "C", "pending", "high", []string{"a"}),
	}
	result := ResolveDependencies(tasks)

	taskMap := make(map[string]types.ResolvedTask)
	for _, rt := range result.Tasks {
		taskMap[rt.ID] = rt
	}

	if taskMap["c"].Classification != "blocked" {
		t.Errorf("c classification = %q, want 'blocked' (dep on cycle participant)", taskMap["c"].Classification)
	}
}

// ---------------------------------------------------------------------------
// ParentID propagation
// ---------------------------------------------------------------------------

func TestBrainEntryToResolvedTask_ParentIDPropagation(t *testing.T) {
	// Test that ParentID flows from BrainEntry to ResolvedTask
	entry := types.BrainEntry{
		ID:        "task123",
		Title:     "Child Task",
		Status:    "pending",
		Priority:  "high",
		ParentID:  "parent456",
		DependsOn: []string{},
		Created:   "2025-01-01T00:00:00Z",
	}

	resolved := brainEntryToResolvedTask(&entry)

	if resolved.ParentID != "parent456" {
		t.Errorf("ParentID = %q, want %q", resolved.ParentID, "parent456")
	}
}

func TestResolveDependencies_ParentIDPreserved(t *testing.T) {
	// Test that ParentID is preserved through full dependency resolution
	tasks := []types.BrainEntry{
		{
			ID:       "parent",
			Title:    "Parent Task",
			Status:   "completed",
			Priority: "high",
			Created:  "2025-01-01T00:00:00Z",
		},
		{
			ID:        "child1",
			Title:     "Child Task 1",
			Status:    "pending",
			Priority:  "medium",
			ParentID:  "parent",
			DependsOn: []string{},
			Created:   "2025-01-01T01:00:00Z",
		},
		{
			ID:        "child2",
			Title:     "Child Task 2",
			Status:    "pending",
			Priority:  "low",
			ParentID:  "parent",
			DependsOn: []string{"child1"},
			Created:   "2025-01-01T02:00:00Z",
		},
	}

	result := ResolveDependencies(tasks)

	taskMap := make(map[string]types.ResolvedTask)
	for _, rt := range result.Tasks {
		taskMap[rt.ID] = rt
	}

	// Parent should have no ParentID
	if taskMap["parent"].ParentID != "" {
		t.Errorf("parent.ParentID = %q, want empty", taskMap["parent"].ParentID)
	}

	// Children should have ParentID
	if taskMap["child1"].ParentID != "parent" {
		t.Errorf("child1.ParentID = %q, want %q", taskMap["child1"].ParentID, "parent")
	}
	if taskMap["child2"].ParentID != "parent" {
		t.Errorf("child2.ParentID = %q, want %q", taskMap["child2"].ParentID, "parent")
	}
}
