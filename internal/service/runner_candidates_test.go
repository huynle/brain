package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func insertCandidateRunner(t *testing.T, store *storage.TenantStore, row storage.RunnerRow) {
	t.Helper()
	if row.Hostname == "" {
		row.Hostname = row.RunnerID + "-host"
	}
	if row.RegisteredAt == 0 {
		row.RegisteredAt = time.Now().UnixMilli()
	}
	if row.LastHeartbeat == 0 {
		row.LastHeartbeat = time.Now().UnixMilli()
	}
	if row.MaxParallel == 0 {
		row.MaxParallel = 1
	}
	if err := store.UpsertRunner(context.Background(), &row); err != nil {
		t.Fatalf("UpsertRunner(%s): %v", row.RunnerID, err)
	}
}

func candidateByID(t *testing.T, got *types.RunnerCandidatesResponse, id string) types.RunnerCandidate {
	t.Helper()
	for _, candidate := range got.Candidates {
		if candidate.Runner.RunnerID == id {
			return candidate
		}
	}
	t.Fatalf("candidate %q not found in %+v", id, got.Candidates)
	return types.RunnerCandidate{}
}

func TestFeatureRunnerCandidatesRequireProjectExecutorAndEveryUnfinishedTaskCapability(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "task-a", "A", "pending", "high", "brain", map[string]any{
		"feature_id":          "feature-one",
		"executor":            "opencode",
		"requires_capability": []string{"go"},
	})
	insertTaskNote(t, store, "task-b", "B", "pending", "medium", "brain", map[string]any{
		"feature_id":          "feature-one",
		"executor":            "opencode",
		"requires_capability": []string{"docker"},
	})
	// A completed task must not constrain a new assignment.
	insertTaskNote(t, store, "task-c", "C", "completed", "low", "brain", map[string]any{
		"feature_id":          "feature-one",
		"requires_capability": []string{"gpu"},
	})

	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "eligible", Projects: []string{"brain"}, Executors: []string{"opencode"}, Capabilities: []string{"go", "docker"}})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "wrong-project", Projects: []string{"other"}, Executors: []string{"opencode"}, Capabilities: []string{"go", "docker"}})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "missing-cap", Projects: []string{"brain"}, Executors: []string{"opencode"}, Capabilities: []string{"go"}})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "wrong-executor", Projects: []string{"brain"}, Executors: []string{"pi"}, Capabilities: []string{"go", "docker"}})

	got, err := svc.GetFeatureRunnerCandidates(ctx, "brain", "feature-one")
	if err != nil {
		t.Fatalf("GetFeatureRunnerCandidates: %v", err)
	}
	if candidate := candidateByID(t, got, "eligible"); !candidate.Compatible {
		t.Fatalf("eligible candidate rejected: %+v", candidate.Reasons)
	}
	if candidate := candidateByID(t, got, "wrong-project"); candidate.Compatible || candidate.Reasons[0].Code != "project_not_allowed" {
		t.Fatalf("wrong-project candidate = %+v", candidate)
	}
	if candidate := candidateByID(t, got, "missing-cap"); candidate.Compatible || candidate.Reasons[0].Code != "missing_task_capability" {
		t.Fatalf("missing-cap candidate = %+v", candidate)
	}
	if candidate := candidateByID(t, got, "wrong-executor"); candidate.Compatible || candidate.Reasons[0].Code != "unsupported_executor" {
		t.Fatalf("wrong-executor candidate = %+v", candidate)
	}
}

func TestFeatureRunnerCandidatesSeparateCompatibilityFromAvailability(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	insertTaskNote(t, store, "task-a", "A", "pending", "high", "brain", map[string]any{"feature_id": "feature-one"})
	insertCandidateRunner(t, store, storage.RunnerRow{
		RunnerID: "offline-compatible", Executors: []string{"opencode"},
		LastHeartbeat: time.Now().Add(-RunnerStaleThreshold - time.Minute).UnixMilli(),
	})

	got, err := svc.GetFeatureRunnerCandidates(context.Background(), "brain", "feature-one")
	if err != nil {
		t.Fatalf("GetFeatureRunnerCandidates: %v", err)
	}
	candidate := candidateByID(t, got, "offline-compatible")
	if !candidate.Compatible || candidate.Available {
		t.Fatalf("candidate compatibility/availability = %t/%t, want true/false", candidate.Compatible, candidate.Available)
	}
}

func TestFeatureAssignmentForceOnlyBypassesTemporaryUnavailability(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	insertTaskNote(t, store, "task-a", "A", "pending", "high", "brain", map[string]any{"feature_id": "feature-one"})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "paused-compatible", Executors: []string{"opencode"}})
	if found, err := store.SetRunnerPaused(context.Background(), "paused-compatible", true); err != nil || !found {
		t.Fatalf("SetRunnerPaused: found=%t err=%v", found, err)
	}

	request := types.FeatureAssignmentRequest{RunnerID: "paused-compatible", Intent: "assign"}
	if _, err := svc.AssignFeatureToRunner(context.Background(), "brain", "feature-one", request); err == nil {
		t.Fatal("expected unavailable runner assignment without force to fail")
	}
	request.Force = true
	if _, err := svc.AssignFeatureToRunner(context.Background(), "brain", "feature-one", request); err != nil {
		t.Fatalf("forced compatible assignment: %v", err)
	}
}

func TestCandidateDurableCompatibilityMatchesSchedulerPredicate(t *testing.T) {
	placement := &types.ProjectPlacement{
		ProjectID: "brain", RequiredCapabilities: []string{"project-cap"},
		RequiredLabels: map[string]string{"pool": "coding"}, Resources: map[string]any{"gpu": 1},
		WorkspacePolicy: types.WorkspacePolicyWorktree, Affinity: types.PlacementAffinityStrict,
		PreferredMachines: []string{"machine-a"},
	}
	task := types.ResolvedTask{
		ID: "task-a", Executor: "opencode", RequiresCapability: []string{"task-cap"},
		MachineAffinity: types.MachineAffinityLocal, OriginMachineID: "machine-a",
	}
	compatible := types.RunnerInfo{
		RunnerID: "runner-a", MachineID: "machine-a", Status: types.RunnerStatusOnline,
		Projects: []string{"brain"}, Executors: []string{"opencode"},
		Capabilities: []string{"project-cap", "task-cap"}, Labels: map[string]string{"pool": "coding"},
		Resources: map[string]any{"gpu": 1}, WorkspaceRoots: []string{"/work"}, MaxParallel: 1, DispatchPush: true,
	}
	mutations := []struct {
		name string
		edit func(*types.RunnerInfo)
	}{
		{"project", func(r *types.RunnerInfo) { r.Projects = []string{"other"} }},
		{"executor", func(r *types.RunnerInfo) { r.Executors = []string{"pi"} }},
		{"task capability", func(r *types.RunnerInfo) { r.Capabilities = []string{"project-cap"} }},
		{"project capability", func(r *types.RunnerInfo) { r.Capabilities = []string{"task-cap"} }},
		{"label", func(r *types.RunnerInfo) { r.Labels = map[string]string{} }},
		{"resource", func(r *types.RunnerInfo) { r.Resources = map[string]any{} }},
		{"workspace", func(r *types.RunnerInfo) { r.WorkspaceRoots = nil }},
		{"project affinity", func(r *types.RunnerInfo) { r.MachineID = "machine-b" }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			runner := compatible
			tc.edit(&runner)
			reasons := durableRunnerCompatibilityReasons("brain", []types.ResolvedTask{task}, runner, placement)
			_, schedulerEligible := runnerEligibleForTask(task, "brain", runner, placement)
			if len(reasons) == 0 || schedulerEligible {
				t.Fatalf("reasons=%+v schedulerEligible=%t", reasons, schedulerEligible)
			}
		})
	}
	if reasons := durableRunnerCompatibilityReasons("brain", []types.ResolvedTask{task}, compatible, placement); len(reasons) != 0 {
		t.Fatalf("compatible runner rejected: %+v", reasons)
	}
	if _, ok := runnerEligibleForTask(task, "brain", compatible, placement); !ok {
		t.Fatal("scheduler rejected compatible runner")
	}
}

func TestAssignFeatureToRunnerRejectsDurablyIncompatibleRunnerEvenWhenForced(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	insertTaskNote(t, store, "task-a", "A", "pending", "high", "brain", map[string]any{
		"feature_id":          "feature-one",
		"requires_capability": []string{"gpu"},
	})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "cpu-only", Executors: []string{"opencode"}})

	_, err := svc.AssignFeatureToRunner(context.Background(), "brain", "feature-one", types.FeatureAssignmentRequest{
		RunnerID: "cpu-only", Intent: "assign", Force: true,
	})
	if err == nil {
		t.Fatal("expected incompatible assignment to fail")
	}
}

func TestProposedTaskRunnerCandidatesSupportPreCreationDiscovery(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "coding", Projects: []string{"brain"}, Executors: []string{"opencode"}, Capabilities: []string{"gpu"}})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "brain-conversation-worker", Projects: []string{"assistant-jobs"}, Executors: []string{"assistant"}})

	got, err := svc.GetProposedTaskRunnerCandidates(context.Background(), "brain", types.TaskRunnerCandidatesRequest{Executor: "opencode", RequiresCapability: []string{"gpu"}})
	if err != nil {
		t.Fatalf("GetProposedTaskRunnerCandidates: %v", err)
	}
	if candidate := candidateByID(t, got, "coding"); !candidate.Compatible {
		t.Fatalf("coding candidate rejected: %+v", candidate)
	}
	if candidate := candidateByID(t, got, "brain-conversation-worker"); candidate.Compatible {
		t.Fatalf("conversation worker must not match: %+v", candidate)
	}
}
