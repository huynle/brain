package service

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Re-scoping a live goal (UpdateGoal + feature_id)
//
// A goal's feature scope used to be write-once: create honored feature_id,
// update dropped it, and the only way to fix a mis-scoped goal was to delete
// and recreate it — destroying its id, entry, and audit history.
// =============================================================================

// rescopeTaskLister is the task fixture these tests scope over: three tasks
// spread across two features plus one with no feature at all.
func rescopeTaskLister() *goalScopeTaskLister {
	return &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "pending", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "pending", FeatureID: "feat-2"},
		{ID: "t3", Title: "C", Status: "pending", FeatureID: ""},
	}}
}

func TestUpdateGoal_FeatureIDRescopesAndClears(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()
	svc := NewGoalService(brain, rescopeTaskLister(), store)

	const goalID = "g-rescope"
	if _, err := svc.CreateGoal(ctx, types.CreateGoalRequest{
		Project:   "proj",
		FeatureID: "feat-1",
		Title:     "Ship the thing",
		Config:    types.GoalConfig{ID: goalID},
		Action:    *defaultGoalAction(),
	}); err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}

	// Point the goal at a different feature.
	moved := "feat-2"
	summary, err := svc.UpdateGoal(ctx, goalID, types.UpdateGoalRequest{FeatureID: &moved})
	if err != nil {
		t.Fatalf("UpdateGoal move: %v", err)
	}
	if summary.FeatureID != "feat-2" {
		t.Errorf("returned summary FeatureID = %q, want feat-2", summary.FeatureID)
	}

	// Re-read from storage rather than trusting the returned summary: the
	// original defect returned a plausible-looking response while persisting
	// nothing.
	goal := requireGoalEntry(t, svc, goalID)
	if goal.FeatureID != "feat-2" {
		t.Fatalf("persisted entry FeatureID = %q, want feat-2", goal.FeatureID)
	}
	if goal.Trigger == nil || goal.Trigger.Filter["feature_id"] != "feat-2" {
		t.Errorf("trigger filter feature_id = %v, want feat-2 (a re-scoped goal must also listen on the new feature)", triggerFilter(goal))
	}
	progress, err := svc.GoalProgress(ctx, goalID)
	if err != nil {
		t.Fatalf("GoalProgress after move: %v", err)
	}
	if progress.Total != 1 || len(progress.Tasks) != 1 || progress.Tasks[0].ID != "t2" {
		t.Errorf("after move progress covers %d task(s) %v, want exactly [t2]", progress.Total, progress.Tasks)
	}

	// Clearing the scope widens the goal back to its whole project. This is
	// the case that forced delete-and-recreate: "" was indistinguishable from
	// an omitted field all the way down.
	cleared := ""
	if _, err := svc.UpdateGoal(ctx, goalID, types.UpdateGoalRequest{FeatureID: &cleared}); err != nil {
		t.Fatalf("UpdateGoal clear: %v", err)
	}
	goal = requireGoalEntry(t, svc, goalID)
	if goal.FeatureID != "" {
		t.Fatalf("persisted entry FeatureID = %q after clear, want empty", goal.FeatureID)
	}
	if goal.Trigger != nil {
		if _, ok := goal.Trigger.Filter["feature_id"]; ok {
			t.Errorf("trigger still filters on feature_id after clear: %v", triggerFilter(goal))
		}
	}
	progress, err = svc.GoalProgress(ctx, goalID)
	if err != nil {
		t.Fatalf("GoalProgress after clear: %v", err)
	}
	if progress.Total != 3 {
		t.Errorf("after clear progress covers %d task(s), want all 3 project tasks", progress.Total)
	}
	if progress.FeatureStatus != "" {
		t.Errorf("FeatureStatus = %q for a project-scoped goal, want empty: it names an aggregate that does not exist", progress.FeatureStatus)
	}
}

func TestUpdateGoal_OmittedFeatureIDLeavesScopeAlone(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()
	svc := NewGoalService(brain, rescopeTaskLister(), store)

	const goalID = "g-keep-scope"
	if _, err := svc.CreateGoal(ctx, types.CreateGoalRequest{
		Project:   "proj",
		FeatureID: "feat-1",
		Title:     "Keep my scope",
		Config:    types.GoalConfig{ID: goalID},
		Action:    *defaultGoalAction(),
	}); err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}

	// An unrelated update must not silently widen the goal to its project.
	title := "Renamed"
	if _, err := svc.UpdateGoal(ctx, goalID, types.UpdateGoalRequest{Title: &title}); err != nil {
		t.Fatalf("UpdateGoal: %v", err)
	}

	goal := requireGoalEntry(t, svc, goalID)
	if goal.FeatureID != "feat-1" {
		t.Errorf("FeatureID = %q after an unrelated update, want feat-1", goal.FeatureID)
	}
	if goal.Title != "Renamed" {
		t.Errorf("Title = %q, want Renamed", goal.Title)
	}
}

// requireGoalEntry re-reads a goal entry from storage by goal ID.
func requireGoalEntry(t *testing.T, svc *GoalService, goalID string) types.BrainEntry {
	t.Helper()
	entry, err := svc.findGoalByID(context.Background(), goalID)
	if err != nil {
		t.Fatalf("findGoalByID(%q): %v", goalID, err)
	}
	return *entry
}

// triggerFilter renders a goal entry's trigger filter for failure messages.
func triggerFilter(goal types.BrainEntry) map[string]string {
	if goal.Trigger == nil {
		return nil
	}
	return goal.Trigger.Filter
}

// =============================================================================
// Progress uses goal semantics, not feature semantics
//
// GoalProgress used to call the feature-level computeTaskStats, whose buckets
// are hardcoded. It reported "cancelled" tasks as blocked no matter what the
// goal's blocked_statuses said — and disagreed with the reconciler even with
// no configuration at all.
// =============================================================================

func TestGoalProgress_HonorsConfiguredStatusSets(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	// The live repro: legacy tasks left cancelled, with blocked_statuses
	// explicitly narrowed to ["blocked"].
	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "Legacy", Status: "cancelled"},
		{ID: "t2", Title: "Legacy 2", Status: "cancelled"},
		{ID: "t3", Title: "Done", Status: "completed"},
		{ID: "t4", Title: "Next", Status: "pending"},
	}}
	svc := NewGoalService(brain, lister, store)

	cfg := types.GoalConfig{
		ID:               "g-statuses",
		CompleteStatuses: []string{"completed", "validated", "cancelled", "superseded", "archived"},
		BlockedStatuses:  []string{"blocked"},
	}
	goalEntryWithConfig(t, brain, "proj", "", "Status-configured goal", cfg, defaultGoalAction())

	progress, err := svc.GoalProgress(ctx, cfg.ID)
	if err != nil {
		t.Fatalf("GoalProgress: %v", err)
	}
	if progress.Blocked != 0 {
		t.Errorf("Blocked = %d, want 0: blocked_statuses is [blocked] and no task is blocked", progress.Blocked)
	}
	if progress.Completed != 3 {
		t.Errorf("Completed = %d, want 3: cancelled is configured as complete", progress.Completed)
	}
	if progress.Total != 4 {
		t.Errorf("Total = %d, want 4", progress.Total)
	}
	if progress.GoalStatus != "pending" {
		t.Errorf("GoalStatus = %q, want pending: one task is still pending and none is blocked", progress.GoalStatus)
	}
}

func TestGoalProgress_DefaultsMatchReconcilerDefaults(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	// No status configuration at all — the case where the two rules used to
	// disagree anyway: computeTaskStats called cancelled blocked and dropped
	// archived from the totals; the reconciler ignores cancelled and counts
	// archived as complete.
	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "Shelved", Status: "archived"},
		{ID: "t2", Title: "Dropped", Status: "cancelled"},
		{ID: "t3", Title: "Next", Status: "pending"},
	}}
	svc := NewGoalService(brain, lister, store)

	cfg := types.GoalConfig{ID: "g-defaults"}
	goalEntryWithConfig(t, brain, "proj", "", "Unconfigured goal", cfg, defaultGoalAction())

	progress, err := svc.GoalProgress(ctx, cfg.ID)
	if err != nil {
		t.Fatalf("GoalProgress: %v", err)
	}
	if progress.Blocked != 0 {
		t.Errorf("Blocked = %d, want 0: cancelled is not a default blocked status", progress.Blocked)
	}
	if progress.Total != 3 {
		t.Errorf("Total = %d, want 3: archived tasks are still linked work the reconciler counts", progress.Total)
	}
	if progress.Completed != 1 {
		t.Errorf("Completed = %d, want 1: archived counts as complete by default", progress.Completed)
	}
}

// TestGoalProgress_AgreesWithReconciler pins the operator-facing view and the
// reconcile decision to one classification. This is the test whose absence let
// the two drift: each was internally consistent, and nothing compared them.
func TestGoalProgress_AgreesWithReconciler(t *testing.T) {
	cases := []struct {
		name         string
		cfg          types.GoalConfig
		tasks        []types.ResolvedTask
		wantDecision ReconcileDecision
		wantStatus   string
	}{
		{
			name:         "cancelled legacy work is neither complete nor blocked",
			cfg:          types.GoalConfig{ID: "g1", BlockedStatuses: []string{"blocked"}},
			tasks:        []types.ResolvedTask{{ID: "a", Status: "cancelled"}, {ID: "b", Status: "pending"}},
			wantDecision: ReconcileNeedWork,
			wantStatus:   "pending",
		},
		{
			name:         "genuinely blocked work",
			cfg:          types.GoalConfig{ID: "g2"},
			tasks:        []types.ResolvedTask{{ID: "a", Status: "blocked"}, {ID: "b", Status: "completed"}},
			wantDecision: ReconcileBlock,
			wantStatus:   "blocked",
		},
		{
			name:         "in progress outranks blocked",
			cfg:          types.GoalConfig{ID: "g3"},
			tasks:        []types.ResolvedTask{{ID: "a", Status: "blocked"}, {ID: "b", Status: "in_progress"}},
			wantDecision: ReconcileNoop,
			wantStatus:   "in_progress",
		},
		{
			name:         "archived counts as complete",
			cfg:          types.GoalConfig{ID: "g4"},
			tasks:        []types.ResolvedTask{{ID: "a", Status: "archived"}, {ID: "b", Status: "completed"}},
			wantDecision: ReconcileComplete,
			wantStatus:   "completed",
		},
		{
			name:         "custom complete set retires cancelled work",
			cfg:          types.GoalConfig{ID: "g5", CompleteStatuses: []string{"completed", "cancelled"}},
			tasks:        []types.ResolvedTask{{ID: "a", Status: "cancelled"}, {ID: "b", Status: "completed"}},
			wantDecision: ReconcileComplete,
			wantStatus:   "completed",
		},
		{
			name:         "no linked tasks",
			cfg:          types.GoalConfig{ID: "g6"},
			tasks:        nil,
			wantDecision: ReconcileNeedWork,
			wantStatus:   "pending",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			brain, store, _ := newTestBrainService(t)
			svc := NewGoalService(brain, &goalScopeTaskLister{tasks: tc.tasks}, store)
			goalEntryWithConfig(t, brain, "proj", "", "Agreement "+tc.cfg.ID, tc.cfg, defaultGoalAction())

			progress, err := svc.GoalProgress(context.Background(), tc.cfg.ID)
			if err != nil {
				t.Fatalf("GoalProgress: %v", err)
			}
			decision, reason := decideReconcile(tc.cfg, tc.tasks)

			if decision != tc.wantDecision {
				t.Errorf("decideReconcile = %q (%s), want %q", decision, reason, tc.wantDecision)
			}
			if progress.GoalStatus != tc.wantStatus {
				t.Errorf("GoalStatus = %q, want %q (reconciler said %q)", progress.GoalStatus, tc.wantStatus, decision)
			}

			// The counts the view reports must be the counts the decision was
			// made from, not a parallel tally.
			stats := goalTaskStats(&tc.cfg, tc.tasks)
			if progress.Total != stats.Total || progress.Completed != stats.Completed ||
				progress.Blocked != stats.Blocked || progress.InProgress != stats.InProgress ||
				progress.Pending != stats.Pending {
				t.Errorf("progress counts %+v disagree with the reconciler's tally %+v", progress, stats)
			}
		})
	}
}
