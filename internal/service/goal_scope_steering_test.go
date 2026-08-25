package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Test doubles
// =============================================================================

// goalScopeTaskLister is a full-capability task lister: it implements
// FeatureTaskLister plus the optional goalProjectTaskLister and
// goalSingleTaskGetter capabilities the scope resolution consults.
type goalScopeTaskLister struct {
	tasks []types.ResolvedTask
}

func (m *goalScopeTaskLister) GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error) {
	var out []types.ResolvedTask
	for _, t := range m.tasks {
		if t.FeatureID == featureID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *goalScopeTaskLister) GetTasks(ctx context.Context, projectID string) (*types.TaskListResponse, error) {
	tasks := append([]types.ResolvedTask(nil), m.tasks...)
	return &types.TaskListResponse{Tasks: tasks, Count: len(tasks)}, nil
}

func (m *goalScopeTaskLister) GetTask(ctx context.Context, projectID, taskID string) (*types.ResolvedTask, error) {
	for _, t := range m.tasks {
		if t.ID == taskID {
			task := t
			return &task, nil
		}
	}
	return nil, fmt.Errorf("task %q not found in project %q", taskID, projectID)
}

// fakeSteerer records SteerTask calls and returns scripted results.
type fakeSteerer struct {
	calls   []fakeSteerCall
	result  SteerResult
	err     error
	results map[string]SteerResult // per-task override by task ID
}

type fakeSteerCall struct {
	projectID string
	taskID    string
	prompt    string
}

func (f *fakeSteerer) SteerTask(ctx context.Context, projectID, taskID, prompt string) (SteerResult, error) {
	f.calls = append(f.calls, fakeSteerCall{projectID: projectID, taskID: taskID, prompt: prompt})
	if f.err != nil {
		return SteerResult{}, f.err
	}
	if r, ok := f.results[taskID]; ok {
		return r, nil
	}
	if f.result == (SteerResult{}) {
		return SteerResult{Steered: true}, nil
	}
	return f.result, nil
}

// fakePauseChecker is a static automation pause gate.
type fakePauseChecker struct{ paused bool }

func (f *fakePauseChecker) IsAutomationsPaused() bool { return f.paused }

// goalEntryWithConfig persists a goal automation entry and returns a
// BrainEntry literal carrying the given config (mirrors saveGoalEntry but
// with full config control).
func goalEntryWithConfig(t *testing.T, brain *BrainServiceImpl, project, featureID, title string, cfg types.GoalConfig, action *types.AutomationAction) types.BrainEntry {
	t.Helper()

	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:        "automation",
		Title:       title,
		Content:     "goal body",
		Status:      "active",
		Project:     project,
		FeatureID:   featureID,
		GeneratedBy: types.GoalGeneratedBy,
		Tags:        []string{types.GoalTag, types.GoalIDTag(cfg.ID)},
		Action:      action,
		Goal:        &cfg,
	})
	if err != nil {
		t.Fatalf("save goal entry: %v", err)
	}

	return types.BrainEntry{
		ID:          resp.ID,
		Path:        resp.Path,
		Title:       title,
		Type:        "automation",
		Status:      "active",
		ProjectID:   project,
		FeatureID:   featureID,
		GeneratedBy: types.GoalGeneratedBy,
		Action:      action,
		Goal:        &cfg,
	}
}

// =============================================================================
// Goal scopes: task > feature > project
// =============================================================================

func TestLinkedTasksForGoal_TaskScope(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "pending", FeatureID: "feat-2"},
		{ID: "t3", Title: "C", Status: "pending", FeatureID: ""},
	}}
	svc := NewGoalService(brain, lister, store)

	// Task scope resolves the one task regardless of its feature.
	goal := types.BrainEntry{ProjectID: "proj", FeatureID: "feat-1",
		Goal: &types.GoalConfig{ID: "g-task", TaskID: "t2"}}
	tasks, err := svc.linkedTasksForGoal(context.Background(), goal)
	if err != nil {
		t.Fatalf("linkedTasksForGoal: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "t2" {
		t.Fatalf("task scope resolved %v, want exactly [t2]", tasks)
	}

	// A missing scope target is empty (need_work), not an error.
	goal.Goal.TaskID = "missing"
	tasks, err = svc.linkedTasksForGoal(context.Background(), goal)
	if err != nil {
		t.Fatalf("linkedTasksForGoal missing task: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("missing task scope resolved %v, want empty", tasks)
	}
}

func TestLinkedTasksForGoal_FeatureScope(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "pending", FeatureID: "feat-2"},
	}}
	svc := NewGoalService(brain, lister, store)

	goal := types.BrainEntry{ProjectID: "proj", FeatureID: "feat-1",
		Goal: &types.GoalConfig{ID: "g-feat"}}
	tasks, err := svc.linkedTasksForGoal(context.Background(), goal)
	if err != nil {
		t.Fatalf("linkedTasksForGoal: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "t1" {
		t.Fatalf("feature scope resolved %v, want exactly [t1]", tasks)
	}
}

func TestLinkedTasksForGoal_ProjectScope(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "pending", FeatureID: "feat-2"},
		{ID: "t3", Title: "C", Status: "pending", FeatureID: ""},
	}}
	svc := NewGoalService(brain, lister, store)

	// No task_id, no feature_id -> ALL project tasks (not just the
	// empty-feature ones, which was the old scope mismatch).
	goal := types.BrainEntry{ProjectID: "proj", Goal: &types.GoalConfig{ID: "g-proj"}}
	tasks, err := svc.linkedTasksForGoal(context.Background(), goal)
	if err != nil {
		t.Fatalf("linkedTasksForGoal: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("project scope resolved %d tasks %v, want all 3", len(tasks), tasks)
	}
}

func TestGoalProgress_AllThreeScopes(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()
	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "in_progress", FeatureID: "feat-2"},
		{ID: "t3", Title: "C", Status: "pending", FeatureID: ""},
	}}
	svc := NewGoalService(brain, lister, store)

	cases := []struct {
		name      string
		featureID string
		cfg       types.GoalConfig
		wantTotal int
		wantTask  string
	}{
		{"task scope", "", types.GoalConfig{ID: "g-scope-task", TaskID: "t2"}, 1, "t2"},
		{"feature scope", "feat-1", types.GoalConfig{ID: "g-scope-feat"}, 1, ""},
		{"project scope", "", types.GoalConfig{ID: "g-scope-proj"}, 3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goalEntryWithConfig(t, brain, "proj", tc.featureID, "Scope goal "+tc.cfg.ID, tc.cfg, defaultGoalAction())

			progress, err := svc.GoalProgress(ctx, tc.cfg.ID)
			if err != nil {
				t.Fatalf("GoalProgress: %v", err)
			}
			if progress.Total != tc.wantTotal {
				t.Errorf("Total = %d, want %d", progress.Total, tc.wantTotal)
			}
			if progress.TaskID != tc.wantTask {
				t.Errorf("TaskID = %q, want %q", progress.TaskID, tc.wantTask)
			}
		})
	}
}

// =============================================================================
// Trigger scope + feature.completed matching (regression)
// =============================================================================

func TestBuildGoalTrigger_TaskScopeFilter(t *testing.T) {
	in := baseGoalInput()
	in.Config.TaskID = "t-42"
	entry, err := BuildGoalAutomation(in)
	if err != nil {
		t.Fatalf("BuildGoalAutomation: %v", err)
	}
	if entry.Trigger.Filter["task_id"] != "t-42" {
		t.Errorf("Filter[task_id] = %q, want t-42", entry.Trigger.Filter["task_id"])
	}
	// Task scope wins over feature scope in the filter.
	if _, ok := entry.Trigger.Filter["feature_id"]; ok {
		t.Errorf("task-scoped trigger must not also filter on feature_id, got %v", entry.Trigger.Filter)
	}
}

func TestBuildGoalTrigger_FeatureSourceOmitsToStatus(t *testing.T) {
	in := baseGoalInput()
	in.Config.TriggerSource = types.GoalTriggerSourceFeature
	entry, err := BuildGoalAutomation(in)
	if err != nil {
		t.Fatalf("BuildGoalAutomation: %v", err)
	}
	// feature.completed events never carry ToStatus; a to_status filter on a
	// feature-only trigger means the goal can never fire.
	if _, ok := entry.Trigger.Filter["to_status"]; ok {
		t.Errorf("feature-only trigger must omit to_status filter, got %v", entry.Trigger.Filter)
	}
}

func TestGoalMatchesEvent_FeatureCompletedWithoutToStatus(t *testing.T) {
	entry, err := BuildGoalAutomation(baseGoalInput()) // source "both": to_status filter present
	if err != nil {
		t.Fatalf("BuildGoalAutomation: %v", err)
	}
	if entry.Trigger.Filter["to_status"] == "" {
		t.Fatalf("precondition: both-source trigger should carry a to_status filter")
	}

	// feature.completed carries no ToStatus and must still match.
	evt := types.Event{
		Type:      types.EventFeatureCompleted,
		ProjectID: "brain-api",
		FeatureID: "auth-system",
	}
	if !goalMatchesEvent(entry, evt) {
		t.Error("feature.completed without ToStatus must match a both-source goal trigger")
	}

	// task.status_changed keeps the status gate.
	taskEvt := types.Event{
		Type:      types.EventTaskStatusChanged,
		ProjectID: "brain-api",
		FeatureID: "auth-system",
		ToStatus:  "pending",
	}
	if goalMatchesEvent(entry, taskEvt) {
		t.Error("task.status_changed with non-matching ToStatus must NOT match")
	}
	taskEvt.ToStatus = "completed"
	if !goalMatchesEvent(entry, taskEvt) {
		t.Error("task.status_changed with matching ToStatus must match")
	}
}

// TestHandleEvent_FeatureSourceGoalReconcilesOnFeatureCompleted is the
// end-to-end regression: a trigger_source=feature goal actually reconciles on
// a feature.completed event (previously dead because of the to_status gate).
func TestHandleEvent_FeatureSourceGoalReconcilesOnFeatureCompleted(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	entry, err := BuildGoalAutomation(GoalInput{
		Project:   "proj",
		FeatureID: "feat-1",
		Title:     "Feature goal",
		Config: types.GoalConfig{
			ID:            "g-feature-src",
			TriggerSource: types.GoalTriggerSourceFeature,
		},
		Action: *defaultGoalAction(),
	})
	if err != nil {
		t.Fatalf("BuildGoalAutomation: %v", err)
	}
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:        entry.Type,
		Title:       entry.Title,
		Content:     entry.Content,
		Status:      entry.Status,
		Project:     entry.ProjectID,
		FeatureID:   entry.FeatureID,
		GeneratedBy: entry.GeneratedBy,
		Tags:        entry.Tags,
		Trigger:     entry.Trigger,
		Action:      entry.Action,
		Goal:        entry.Goal,
	}); err != nil {
		t.Fatalf("save goal automation: %v", err)
	}

	svc := NewGoalService(brain, &goalScopeTaskLister{}, store)

	// A real feature.completed event: no ToStatus (matches production emitters).
	err = svc.HandleEvent(ctx, types.Event{
		Type:      types.EventFeatureCompleted,
		ID:        "evt_feature_completed",
		ProjectID: "proj",
		FeatureID: "feat-1",
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	tasks := listGeneratedGoalTasks(t, brain, "proj")
	if len(tasks) != 1 {
		t.Fatalf("generated task count = %d, want 1 (feature-source goal must reconcile on feature.completed)", len(tasks))
	}
}

// =============================================================================
// Rich prompt composition
// =============================================================================

func TestGenerateGoalTask_RichPrompt(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	cfg := types.GoalConfig{
		ID:         "g-prompt",
		Criteria:   "All auth endpoints return 200",
		Validation: "go test ./internal/auth/...",
		Workdir:    "/tmp/goal-work",
	}
	goal := goalEntryWithConfig(t, brain, "proj", "feat-1", "Ship auth", cfg, defaultGoalAction())
	goal.MergeTargetBranch = "main"
	goal.MergePolicy = "auto_pr"
	goal.Action.Executor = "pi"

	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "Login endpoint", Status: "pending", FeatureID: "feat-1"},
	}}
	svc := NewGoalService(brain, lister, store)

	audit, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	// One pending task, none active -> need_work -> generated task.
	if audit.GeneratedTaskID == "" {
		t.Fatalf("expected a generated task, decision=%s reason=%s", audit.Decision, audit.Reason)
	}

	task, err := brain.Recall(ctx, audit.GeneratedTaskID)
	if err != nil {
		t.Fatalf("recall generated task: %v", err)
	}

	for _, want := range []string{
		"do the goal work",                     // Action.DirectPrompt preserved
		"## Goal",                              // goal section
		"Ship auth",                            // title
		"All auth endpoints return 200",        // criteria
		"go test ./internal/auth/...",          // validation
		"Login endpoint",                       // linked-task snapshot
		"(pending)",                            // snapshot status
		"This task exists to achieve the goal", // purpose instruction
		"self-assessment",                      // self-assess instruction
	} {
		if !strings.Contains(task.DirectPrompt, want) {
			t.Errorf("DirectPrompt missing %q\n---\n%s", want, task.DirectPrompt)
		}
	}

	// Executor + git/merge propagation (as automation_service.createTask does).
	if task.Executor != "pi" {
		t.Errorf("generated task Executor = %q, want pi", task.Executor)
	}
	if task.MergeTargetBranch != "main" {
		t.Errorf("generated task MergeTargetBranch = %q, want main", task.MergeTargetBranch)
	}
	if task.MergePolicy != "auto_pr" {
		t.Errorf("generated task MergePolicy = %q, want auto_pr", task.MergePolicy)
	}
}

// =============================================================================
// Termination
// =============================================================================

func TestReconcile_Complete_TerminatesGoal(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	cfg := types.GoalConfig{ID: "g-terminate"}
	goal := goalEntryWithConfig(t, brain, "proj", "feat-1", "Terminating goal", cfg, defaultGoalAction())
	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
	}}
	svc := NewGoalService(brain, lister, store)

	audit, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if audit.Decision != ReconcileComplete {
		t.Fatalf("decision = %q, want complete", audit.Decision)
	}

	// The entry status flipped to completed...
	entry, err := brain.Recall(ctx, goal.ID)
	if err != nil {
		t.Fatalf("recall goal: %v", err)
	}
	if entry.Status != "completed" {
		t.Errorf("goal entry status = %q, want completed", entry.Status)
	}

	// ...so it no longer participates in event dispatch...
	active, err := svc.listActiveGoals(ctx)
	if err != nil {
		t.Fatalf("listActiveGoals: %v", err)
	}
	for _, g := range active {
		if g.Goal != nil && g.Goal.ID == "g-terminate" {
			t.Error("completed goal must not appear in active dispatch listing")
		}
	}

	// ...but remains visible in the default list and can be reactivated.
	summaries, err := svc.ListGoals(ctx, "proj", "")
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	found := false
	for _, s := range summaries {
		if s.GoalID == "g-terminate" && s.Status == "completed" {
			found = true
		}
	}
	if !found {
		t.Errorf("completed goal missing from default list: %+v", summaries)
	}

	activeStatus := "active"
	if _, err := svc.UpdateGoal(ctx, "g-terminate", UpdateGoalRequest{Status: &activeStatus}); err != nil {
		t.Fatalf("reactivate completed goal: %v", err)
	}
	entry, err = brain.Recall(ctx, goal.ID)
	if err != nil {
		t.Fatalf("recall reactivated goal: %v", err)
	}
	if entry.Status != "active" {
		t.Errorf("reactivated goal status = %q, want active", entry.Status)
	}
}

// =============================================================================
// Steering
// =============================================================================

// steeringGoalSetup persists a goal + in_progress linked work and returns
// (svc, goal, steerer).
func steeringGoalSetup(t *testing.T, steerer SessionSteerer, cfg types.GoalConfig, opts ...GoalServiceOption) (*GoalService, types.BrainEntry, *goalScopeTaskLister) {
	t.Helper()
	brain, store, _ := newTestBrainService(t)

	goal := goalEntryWithConfig(t, brain, "proj", "feat-1", "Steered goal", cfg, defaultGoalAction())
	lister := &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "in_progress", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "pending", FeatureID: "feat-1"},
	}}

	allOpts := opts
	if steerer != nil {
		allOpts = append([]GoalServiceOption{WithGoalSteerer(steerer)}, opts...)
	}
	svc := NewGoalService(brain, lister, store, allOpts...)
	return svc, goal, lister
}

func TestReconcile_Steer_WritesAuditAndPrompts(t *testing.T) {
	steerer := &fakeSteerer{}
	cfg := types.GoalConfig{ID: "g-steer", Criteria: "Everything green", Validation: "just check"}
	svc, goal, _ := steeringGoalSetup(t, steerer, cfg)

	audit, err := svc.Reconcile(context.Background(), goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if audit.Decision != ReconcileSteer {
		t.Fatalf("decision = %q, want steer", audit.Decision)
	}
	if audit.SessionsSteered != 1 || audit.SessionsSkipped != 0 {
		t.Errorf("steered/skipped = %d/%d, want 1/0", audit.SessionsSteered, audit.SessionsSkipped)
	}
	// Only the in_progress task is steered.
	if len(steerer.calls) != 1 || steerer.calls[0].taskID != "t1" {
		t.Fatalf("steer calls = %+v, want exactly [t1]", steerer.calls)
	}
	prompt := steerer.calls[0].prompt
	for _, want := range []string{"Steered goal", "Everything green", "just check", "Self-assess", "correct course"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("steering prompt missing %q\n---\n%s", want, prompt)
		}
	}

	// The persisted audit carries the steer decision too.
	persisted := findGoalReconcileAudit(t, svc.store)
	if persisted.Decision != ReconcileSteer {
		t.Errorf("persisted decision = %q, want steer", persisted.Decision)
	}
	if persisted.SessionsSteered != 1 {
		t.Errorf("persisted SessionsSteered = %d, want 1", persisted.SessionsSteered)
	}
}

func TestReconcile_Steer_CooldownHonored(t *testing.T) {
	steerer := &fakeSteerer{}
	cfg := types.GoalConfig{ID: "g-cooldown", Steering: &types.GoalSteering{CooldownMinutes: 10}}
	svc, goal, _ := steeringGoalSetup(t, steerer, cfg)
	ctx := context.Background()

	// First reconcile steers and stamps last_steered_at.
	audit, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if audit.Decision != ReconcileSteer {
		t.Fatalf("first decision = %q, want steer", audit.Decision)
	}
	if len(steerer.calls) != 1 {
		t.Fatalf("steer calls after first reconcile = %d, want 1", len(steerer.calls))
	}

	// Second reconcile inside the cooldown window: plain noop, no new calls.
	audit, err = svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if audit.Decision != ReconcileNoop {
		t.Errorf("second decision = %q, want noop (cooldown active)", audit.Decision)
	}
	if len(steerer.calls) != 1 {
		t.Errorf("steer calls after cooldown-gated reconcile = %d, want still 1", len(steerer.calls))
	}

	// Advance the clock past the cooldown: steering resumes.
	svc.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	audit, err = svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("third Reconcile: %v", err)
	}
	if audit.Decision != ReconcileSteer {
		t.Errorf("third decision = %q, want steer (cooldown elapsed)", audit.Decision)
	}
	if len(steerer.calls) != 2 {
		t.Errorf("steer calls after cooldown elapsed = %d, want 2", len(steerer.calls))
	}
}

func TestReconcile_Steer_FailureDoesNotFailReconcile(t *testing.T) {
	steerer := &fakeSteerer{err: fmt.Errorf("bridge down")}
	svc, goal, _ := steeringGoalSetup(t, steerer, types.GoalConfig{ID: "g-steer-fail"})

	audit, err := svc.Reconcile(context.Background(), goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile must not fail on steering errors, got: %v", err)
	}
	if audit.Decision != ReconcileSteer {
		t.Errorf("decision = %q, want steer (attempt recorded)", audit.Decision)
	}
	if audit.SessionsSteered != 0 || audit.SessionsSkipped != 1 {
		t.Errorf("steered/skipped = %d/%d, want 0/1", audit.SessionsSteered, audit.SessionsSkipped)
	}
}

func TestReconcile_Steer_UnsupportedExecutorSkips(t *testing.T) {
	steerer := &fakeSteerer{result: SteerResult{Unsupported: true, Reason: "pi has no prompt endpoint"}}
	svc, goal, _ := steeringGoalSetup(t, steerer, types.GoalConfig{ID: "g-steer-pi"})

	audit, err := svc.Reconcile(context.Background(), goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if audit.SessionsSteered != 0 || audit.SessionsSkipped != 1 {
		t.Errorf("steered/skipped = %d/%d, want 0/1 (unsupported executor)", audit.SessionsSteered, audit.SessionsSkipped)
	}
}

func TestReconcile_NilSteererIsNoop(t *testing.T) {
	svc, goal, _ := steeringGoalSetup(t, nil, types.GoalConfig{ID: "g-no-steerer"})

	audit, err := svc.Reconcile(context.Background(), goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if audit.Decision != ReconcileNoop {
		t.Errorf("decision = %q, want noop (no steerer wired)", audit.Decision)
	}
	if audit.SessionsSteered != 0 || audit.SessionsSkipped != 0 {
		t.Errorf("steered/skipped = %d/%d, want 0/0", audit.SessionsSteered, audit.SessionsSkipped)
	}
}

func TestReconcile_Steer_DisabledByConfig(t *testing.T) {
	steerer := &fakeSteerer{}
	off := false
	svc, goal, _ := steeringGoalSetup(t, steerer, types.GoalConfig{
		ID:       "g-steer-off",
		Steering: &types.GoalSteering{Enabled: &off},
	})

	audit, err := svc.Reconcile(context.Background(), goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if audit.Decision != ReconcileNoop {
		t.Errorf("decision = %q, want noop (steering disabled)", audit.Decision)
	}
	if len(steerer.calls) != 0 {
		t.Errorf("steer calls = %d, want 0 (disabled)", len(steerer.calls))
	}
}

// =============================================================================
// Pause gate (guards)
// =============================================================================

func TestReconcile_Paused_SkipsGenerationAndSteering(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()
	steerer := &fakeSteerer{}
	pause := &fakePauseChecker{paused: true}

	// need_work goal: generation must be skipped while paused.
	needWork := goalEntryWithConfig(t, brain, "proj", "feat-none", "Paused needwork goal",
		types.GoalConfig{ID: "g-paused-gen"}, defaultGoalAction())
	svc := NewGoalService(brain, &goalScopeTaskLister{}, store,
		WithGoalSteerer(steerer), WithGoalPauseChecker(pause))

	audit, err := svc.Reconcile(ctx, needWork, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile (need_work, paused): %v", err)
	}
	if audit.GeneratedTaskID != "" {
		t.Errorf("paused reconcile generated task %q, want none", audit.GeneratedTaskID)
	}
	if !strings.Contains(audit.Reason, "paused") {
		t.Errorf("paused audit reason %q must mention paused", audit.Reason)
	}
	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 0 {
		t.Errorf("generated task count = %d, want 0 while paused", len(tasks))
	}

	// noop goal with in_progress work: steering must be skipped while paused.
	steered := goalEntryWithConfig(t, brain, "proj", "feat-1", "Paused steering goal",
		types.GoalConfig{ID: "g-paused-steer"}, defaultGoalAction())
	svc.tasks = &goalScopeTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "in_progress", FeatureID: "feat-1"},
	}}
	audit, err = svc.Reconcile(ctx, steered, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile (noop, paused): %v", err)
	}
	if audit.Decision != ReconcileNoop {
		t.Errorf("paused steering decision = %q, want noop", audit.Decision)
	}
	if !strings.Contains(audit.Reason, "paused") {
		t.Errorf("paused steering audit reason %q must mention paused", audit.Reason)
	}
	if len(steerer.calls) != 0 {
		t.Errorf("steer calls = %d, want 0 while paused", len(steerer.calls))
	}
}

// =============================================================================
// Cron guard (guards)
// =============================================================================

// TestCheckScheduled_SkipsGoalAutomations verifies the cron dispatcher never
// generates tasks for goal automations, even when a goal entry carries a
// cron trigger — goals are driven exclusively by the reconcile loop.
func TestCheckScheduled_SkipsGoalAutomations(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// A goal automation with an always-matching cron trigger.
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:        "automation",
		Title:       "Cron goal",
		Content:     "goal body",
		Status:      "active",
		Project:     "proj",
		GeneratedBy: types.GoalGeneratedBy,
		Tags:        []string{types.GoalTag, types.GoalIDTag("g-cron")},
		Trigger:     &types.TriggerConfig{Type: "cron", Schedule: "* * * * *"},
		Action:      defaultGoalAction(),
		Goal:        &types.GoalConfig{ID: "g-cron"},
	}); err != nil {
		t.Fatalf("save cron goal: %v", err)
	}

	autoSvc := NewAutomationService(brain)
	if err := autoSvc.CheckScheduled(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "proj", Limit: 100})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("cron dispatcher generated %d task(s) for a goal automation, want 0", len(resp.Entries))
	}
}

// =============================================================================
// Periodic reconcile
// =============================================================================

func TestReconcileAllActive_PeriodicAudit(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	// Persist a full goal automation so listActiveGoals returns it with its
	// Goal config rehydrated.
	saveFullGoalAutomation(t, brain, "proj", "feat-1", "g-periodic", "Periodic goal")

	svc := NewGoalService(brain, &goalScopeTaskLister{}, store)
	if err := svc.ReconcileAllActive(ctx); err != nil {
		t.Fatalf("ReconcileAllActive: %v", err)
	}

	// need_work path generated a task, and the audit is stamped "periodic".
	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 1 {
		t.Fatalf("generated task count = %d, want 1", len(tasks))
	}
	audit := findGoalReconcileAudit(t, store)
	if audit.TriggeringEvent != "periodic" {
		t.Errorf("TriggeringEvent = %q, want periodic", audit.TriggeringEvent)
	}
}

// TestStart_PeriodicTickerReconciles shrinks goalReconcileInterval and
// verifies the Start loop's ticker drives reconciles without any events.
func TestStart_PeriodicTickerReconciles(t *testing.T) {
	brain, store, _ := newTestBrainService(t)

	oldInterval := goalReconcileInterval
	goalReconcileInterval = 25 * time.Millisecond
	defer func() { goalReconcileInterval = oldInterval }()

	saveFullGoalAutomation(t, brain, "proj", "feat-1", "g-ticker", "Ticker goal")
	svc := NewGoalService(brain, &goalScopeTaskLister{}, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Start(ctx, realtime.NewEventHub())
	}()

	deadline := time.After(3 * time.Second)
	for {
		if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("periodic ticker did not reconcile the active goal within 3s")
		case <-time.After(25 * time.Millisecond):
		}
	}
	cancel()
	<-done
}

// =============================================================================
// Audit-failure reorder (guards)
// =============================================================================

// TestReconcile_AuditFailureAfterTaskCreationWarnsOnly closes the audit store
// so InsertEvent fails, then verifies a reconcile that generated a task still
// succeeds (the side effect wins; the audit failure only warns).
func TestReconcile_AuditFailureAfterTaskCreationWarnsOnly(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	// A second, closed store: brain writes still work, audit inserts fail.
	_, deadStore, _ := newTestBrainService(t)
	if err := deadStore.Close(); err != nil {
		t.Fatalf("close dead store: %v", err)
	}

	goal := goalEntryWithConfig(t, brain, "proj", "feat-none", "Audit fail goal",
		types.GoalConfig{ID: "g-audit-fail"}, defaultGoalAction())
	svc := NewGoalService(brain, &goalScopeTaskLister{}, deadStore)

	audit, err := svc.Reconcile(context.Background(), goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile must not fail when the audit write fails after task creation, got: %v", err)
	}
	if audit.GeneratedTaskID == "" {
		t.Fatal("expected a generated task despite the audit failure")
	}
	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 1 {
		t.Fatalf("generated task count = %d, want 1", len(tasks))
	}
}

// =============================================================================
// goal_audit: missing goal vs. goal with no history
// =============================================================================

// TestGoalAuditHistory_UnknownGoalIsNotFound closes the last gap in the goal
// API's not-found handling. GoalAuditHistory was the ONE goal method that never
// called findGoalByID — GoalProgress, RunGoal, UpdateGoal and DeleteGoal all do
// — so a typo'd or deleted goal id rendered "No reconcile audit records found
// for goal X", which is byte-identical to what a real goal that has simply never
// reconciled produces.
func TestGoalAuditHistory_UnknownGoalIsNotFound(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	svc := NewGoalService(brain, &goalScopeTaskLister{}, store)

	_, err := svc.GoalAuditHistory(context.Background(), "no-such-goal", 50)
	if !errors.Is(err, types.ErrGoalNotFound) {
		t.Errorf("GoalAuditHistory(unknown) error = %v, want ErrGoalNotFound", err)
	}
}

// TestGoalAuditHistory_ExistingGoalWithNoHistoryIsEmptyNotError is the other
// half: a real goal that has not reconciled yet must still return an empty
// history rather than an error. Making unknown ids fail must not turn a
// legitimately empty history into a failure.
func TestGoalAuditHistory_ExistingGoalWithNoHistoryIsEmptyNotError(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	svc := NewGoalService(brain, &goalScopeTaskLister{}, store)

	created, err := svc.CreateGoal(context.Background(), types.CreateGoalRequest{
		Project: "proj",
		Title:   "Ship it",
		Config:  types.GoalConfig{ID: "ship-it", Criteria: "tests pass"},
		Action:  types.AutomationAction{Type: "create_task"},
	})
	if err != nil {
		t.Fatalf("CreateGoal failed: %v", err)
	}

	history, err := svc.GoalAuditHistory(context.Background(), created.GoalID, 50)
	if err != nil {
		t.Fatalf("GoalAuditHistory(existing) unexpected error: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected empty history for a goal that has not reconciled, got %d", len(history))
	}
}
