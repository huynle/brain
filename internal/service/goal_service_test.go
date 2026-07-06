package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// task is a tiny helper to build a ResolvedTask literal for table tests.
func task(id, title, status string) types.ResolvedTask {
	return types.ResolvedTask{ID: id, Title: title, Status: status}
}

func TestDecideReconcile(t *testing.T) {
	tests := []struct {
		name string
		cfg  types.GoalConfig
		// tasks are the goal's linked tasks.
		tasks []types.ResolvedTask
		// want is the expected decision.
		want ReconcileDecision
	}{
		{
			name:  "empty tasks -> need_work",
			cfg:   types.GoalConfig{ID: "g1"},
			tasks: nil,
			want:  ReconcileNeedWork,
		},
		{
			name: "all completed -> complete",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "completed"),
			},
			want: ReconcileComplete,
		},
		{
			name: "mix of completed+validated all in complete set -> complete",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "validated"),
				task("t3", "C", "completed"),
			},
			want: ReconcileComplete,
		},
		{
			name: "one in_progress others pending/completed -> noop",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "in_progress"),
				task("t3", "C", "pending"),
			},
			want: ReconcileNoop,
		},
		{
			name: "one blocked none in_progress -> block",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "blocked"),
				task("t3", "C", "pending"),
			},
			want: ReconcileBlock,
		},
		{
			name: "all pending none active -> need_work",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "pending"),
				task("t2", "B", "pending"),
			},
			want: ReconcileNeedWork,
		},
		{
			name: "custom complete set only validated; completed task does not count -> not complete",
			cfg:  types.GoalConfig{ID: "g1", CompleteStatuses: []string{"validated"}},
			tasks: []types.ResolvedTask{
				task("t1", "A", "validated"),
				task("t2", "B", "completed"),
			},
			// "completed" is not in the custom complete set, and is not
			// in_progress/blocked/pending -> falls through to need_work.
			want: ReconcileNeedWork,
		},
		{
			name: "blocked + in_progress together -> noop (precedence)",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "in_progress"),
				task("t2", "B", "blocked"),
			},
			want: ReconcileNoop,
		},
		{
			name: "custom blocked set; cancelled counts as blocked -> block",
			cfg:  types.GoalConfig{ID: "g1", BlockedStatuses: []string{"blocked", "cancelled"}},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "cancelled"),
			},
			want: ReconcileBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := decideReconcile(tt.cfg, tt.tasks)
			if got != tt.want {
				t.Errorf("decideReconcile() decision = %q, want %q (reason: %q)", got, tt.want, reason)
			}
			if reason == "" {
				t.Errorf("decideReconcile() returned empty reason for decision %q", got)
			}
		})
	}
}

func TestLinkedTaskSnapshot(t *testing.T) {
	tasks := []types.ResolvedTask{
		task("t1", "First", "completed"),
		task("t2", "Second", "blocked"),
	}

	got := linkedTaskSnapshot(tasks)
	want := []LinkedTaskSnapshot{
		{ID: "t1", Title: "First", Status: "completed"},
		{ID: "t2", Title: "Second", Status: "blocked"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linkedTaskSnapshot() = %#v, want %#v", got, want)
	}
}

func TestLinkedTaskSnapshot_EmptyIsNonNil(t *testing.T) {
	got := linkedTaskSnapshot(nil)
	if got == nil {
		t.Fatal("linkedTaskSnapshot(nil) = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("linkedTaskSnapshot(nil) len = %d, want 0", len(got))
	}

	got = linkedTaskSnapshot([]types.ResolvedTask{})
	if got == nil {
		t.Fatal("linkedTaskSnapshot([]) = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("linkedTaskSnapshot([]) len = %d, want 0", len(got))
	}
}

// =============================================================================
// Reconcile orchestration tests (Phase 2/3)
// =============================================================================

// saveGoalEntry saves a real goal automation entry (so it has a resolvable ID
// for metadata/notes mirroring) and returns the saved id plus a BrainEntry
// value with the Goal config populated to pass into Reconcile.
//
// NOTE: CreateEntryRequest does not carry a GoalConfig field, so brain.Save
// does NOT round-trip Goal. We therefore construct the BrainEntry literal we
// pass to Reconcile ourselves, using the saved id so mirror writes resolve.
func saveGoalEntry(t *testing.T, brain *BrainServiceImpl, project, featureID, goalID, title string, action *types.AutomationAction) types.BrainEntry {
	t.Helper()
	ctx := context.Background()

	resp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:        "automation",
		Title:       title,
		Content:     "goal body",
		Status:      "active",
		Project:     project,
		FeatureID:   featureID,
		GeneratedBy: types.GoalGeneratedBy,
		Tags:        []string{types.GoalTag, types.GoalIDTag(goalID)},
		Action:      action,
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
		Goal: &types.GoalConfig{
			ID:      goalID,
			Workdir: "/tmp/goal-work",
		},
	}
}

func defaultGoalAction() *types.AutomationAction {
	return &types.AutomationAction{
		Type:         "prompt",
		DirectPrompt: "do the goal work",
		Agent:        "tdd-dev",
		Model:        "anthropic/claude-sonnet-4",
	}
}

// listGeneratedGoalTasks returns task entries generated by the goal subsystem.
func listGeneratedGoalTasks(t *testing.T, brain *BrainServiceImpl, project string) []types.BrainEntry {
	t.Helper()
	resp, err := brain.List(context.Background(), types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   1000,
	})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var out []types.BrainEntry
	for _, e := range resp.Entries {
		if e.GeneratedBy == types.GoalGeneratedBy {
			out = append(out, e)
		}
	}
	return out
}

// findGoalReconcileAudit reads back the persisted reconcile audit event from
// the event_log and unmarshals its payload.
func findGoalReconcileAudit(t *testing.T, store *storage.StorageLayer) GoalReconcileAudit {
	t.Helper()
	rows, err := store.GetUnprocessed(context.Background())
	if err != nil {
		t.Fatalf("get unprocessed events: %v", err)
	}
	var found *storage.EventRow
	for _, r := range rows {
		if r.EventType == types.EventGoalReconcile {
			found = r
			break
		}
	}
	if found == nil {
		t.Fatalf("no %q event found in event_log (got %d events)", types.EventGoalReconcile, len(rows))
	}
	var audit GoalReconcileAudit
	if err := json.Unmarshal([]byte(found.Payload), &audit); err != nil {
		t.Fatalf("unmarshal audit payload: %v\npayload=%s", err, found.Payload)
	}
	return audit
}

func TestReconcile_NeedWork_GeneratesTask(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	goal := saveGoalEntry(t, brain, "proj", "feat-1", "g-need", "Ship feature", defaultGoalAction())
	// empty lister -> need_work
	lister := &mockFeatureTaskLister{}
	svc := NewGoalService(brain, lister, store)

	audit, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged, ID: "evt_1"})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	if audit.Decision != ReconcileNeedWork {
		t.Fatalf("decision = %q, want %q", audit.Decision, ReconcileNeedWork)
	}
	if audit.GeneratedTaskID == "" {
		t.Errorf("GeneratedTaskID is empty, want a generated task id")
	}

	tasks := listGeneratedGoalTasks(t, brain, "proj")
	if len(tasks) != 1 {
		t.Fatalf("generated task count = %d, want 1", len(tasks))
	}
	if tasks[0].GeneratedKey == "" {
		t.Errorf("generated task GeneratedKey is empty")
	}
	if tasks[0].DirectPrompt != "do the goal work" {
		t.Errorf("generated task DirectPrompt = %q, want %q", tasks[0].DirectPrompt, "do the goal work")
	}
}

func TestReconcile_NeedWork_DedupesOpenTask(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	goal := saveGoalEntry(t, brain, "proj", "feat-1", "g-dedup", "Ship feature", defaultGoalAction())
	lister := &mockFeatureTaskLister{}
	svc := NewGoalService(brain, lister, store)

	// First reconcile generates a task.
	a1, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("first Reconcile error: %v", err)
	}
	if a1.GeneratedTaskID == "" {
		t.Fatalf("first reconcile did not generate a task")
	}

	// Second reconcile must NOT create a duplicate while the first is open.
	a2, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("second Reconcile error: %v", err)
	}
	if a2.GeneratedTaskID != "" {
		t.Errorf("second reconcile generated task id %q, want empty (deduped)", a2.GeneratedTaskID)
	}

	tasks := listGeneratedGoalTasks(t, brain, "proj")
	if len(tasks) != 1 {
		t.Fatalf("generated task count = %d, want 1 (no duplicate)", len(tasks))
	}
}

func TestReconcile_Complete_NoTaskGenerated(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	goal := saveGoalEntry(t, brain, "proj", "feat-1", "g-complete", "Ship feature", defaultGoalAction())
	lister := &mockFeatureTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "validated", FeatureID: "feat-1"},
	}}
	svc := NewGoalService(brain, lister, store)

	audit, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventFeatureCompleted})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if audit.Decision != ReconcileComplete {
		t.Fatalf("decision = %q, want %q", audit.Decision, ReconcileComplete)
	}
	if audit.GeneratedTaskID != "" {
		t.Errorf("GeneratedTaskID = %q, want empty", audit.GeneratedTaskID)
	}
	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 0 {
		t.Errorf("generated task count = %d, want 0", len(tasks))
	}
	if len(audit.LinkedTasks) != 2 {
		t.Errorf("LinkedTasks len = %d, want 2", len(audit.LinkedTasks))
	}
}

func TestReconcile_Block_NoTaskGenerated(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	goal := saveGoalEntry(t, brain, "proj", "feat-1", "g-block", "Ship feature", defaultGoalAction())
	lister := &mockFeatureTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "blocked", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "pending", FeatureID: "feat-1"},
	}}
	svc := NewGoalService(brain, lister, store)

	audit, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if audit.Decision != ReconcileBlock {
		t.Fatalf("decision = %q, want %q", audit.Decision, ReconcileBlock)
	}
	if audit.GeneratedTaskID != "" {
		t.Errorf("GeneratedTaskID = %q, want empty", audit.GeneratedTaskID)
	}
	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 0 {
		t.Errorf("generated task count = %d, want 0", len(tasks))
	}
}

func TestReconcile_Noop_NoTaskGenerated(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	goal := saveGoalEntry(t, brain, "proj", "feat-1", "g-noop", "Ship feature", defaultGoalAction())
	lister := &mockFeatureTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "in_progress", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "pending", FeatureID: "feat-1"},
	}}
	svc := NewGoalService(brain, lister, store)

	audit, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventTaskStatusChanged})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if audit.Decision != ReconcileNoop {
		t.Fatalf("decision = %q, want %q", audit.Decision, ReconcileNoop)
	}
	if audit.GeneratedTaskID != "" {
		t.Errorf("GeneratedTaskID = %q, want empty", audit.GeneratedTaskID)
	}
	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 0 {
		t.Errorf("generated task count = %d, want 0", len(tasks))
	}
}

func TestReconcile_PersistsAudit(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	goal := saveGoalEntry(t, brain, "proj", "feat-1", "g-audit", "Ship feature", defaultGoalAction())
	lister := &mockFeatureTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "validated", FeatureID: "feat-1"},
	}}
	svc := NewGoalService(brain, lister, store)

	if _, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventFeatureCompleted, ID: "evt_audit"}); err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	audit := findGoalReconcileAudit(t, store)
	if audit.Decision != ReconcileComplete {
		t.Errorf("persisted Decision = %q, want %q", audit.Decision, ReconcileComplete)
	}
	if audit.GoalID != "g-audit" {
		t.Errorf("persisted GoalID = %q, want %q", audit.GoalID, "g-audit")
	}
	if audit.TriggeringEvent != types.EventFeatureCompleted {
		t.Errorf("persisted TriggeringEvent = %q, want %q", audit.TriggeringEvent, types.EventFeatureCompleted)
	}
	if audit.EventID != "evt_audit" {
		t.Errorf("persisted EventID = %q, want %q", audit.EventID, "evt_audit")
	}
	if len(audit.LinkedTasks) != 2 {
		t.Errorf("persisted LinkedTasks len = %d, want 2", len(audit.LinkedTasks))
	}
}

func TestReconcile_ManualTriggerWhenEventEmpty(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	goal := saveGoalEntry(t, brain, "proj", "feat-1", "g-manual", "Ship feature", defaultGoalAction())
	lister := &mockFeatureTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
	}}
	svc := NewGoalService(brain, lister, store)

	audit, err := svc.Reconcile(ctx, goal, types.Event{})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if audit.TriggeringEvent != "manual" {
		t.Errorf("TriggeringEvent = %q, want %q", audit.TriggeringEvent, "manual")
	}
}

func TestReconcile_MirrorsToEntry(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	goal := saveGoalEntry(t, brain, "proj", "feat-1", "g-mirror", "Ship feature", defaultGoalAction())
	lister := &mockFeatureTaskLister{tasks: []types.ResolvedTask{
		{ID: "t1", Title: "A", Status: "completed", FeatureID: "feat-1"},
		{ID: "t2", Title: "B", Status: "validated", FeatureID: "feat-1"},
	}}
	svc := NewGoalService(brain, lister, store)

	if _, err := svc.Reconcile(ctx, goal, types.Event{Type: types.EventFeatureCompleted}); err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// last_reconcile metadata is DB-only; verify via storage row metadata.
	row, err := store.GetNoteByShortID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("get note by id: %v", err)
	}
	if row == nil {
		t.Fatalf("goal entry not found by id %q", goal.ID)
	}
	if !strings.Contains(row.Metadata, "last_reconcile") {
		t.Errorf("entry metadata missing last_reconcile; metadata=%s", row.Metadata)
	}

	// "## Reconciliation Notes" should be appended to the entry content.
	entry, err := brain.Recall(ctx, goal.ID)
	if err != nil {
		t.Fatalf("recall goal: %v", err)
	}
	if !strings.Contains(entry.Content, "## Reconciliation Notes") {
		t.Errorf("entry content missing reconciliation notes section; content=%q", entry.Content)
	}
}

func TestReconcile_GuardNonGoalEntry(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	svc := NewGoalService(brain, &mockFeatureTaskLister{}, store)

	nonGoal := types.BrainEntry{ID: "x", Type: "automation"} // Goal == nil
	if _, err := svc.Reconcile(ctx, nonGoal, types.Event{}); err == nil {
		t.Fatal("Reconcile on entry with Goal==nil returned nil error, want error")
	}
}

// =============================================================================
// UpdateGoal trigger_source persistence round-trip (regression)
//
// Regression: saving the goal config modal updated the automation trigger
// (events/filter) but did NOT persist the rebuilt goal.trigger_source config
// field, so the entry's goal.trigger_source drifted from its trigger. These
// tests re-read goals from storage (via ListGoals -> goalSummaryFromEntry,
// which reads e.Goal/e.Trigger from the persisted entry) and assert both the
// persisted Config.TriggerSource AND the persisted Trigger events match the
// updated trigger source. BuildGoalAutomation is the single source of truth.
// =============================================================================

func TestUpdateGoal_PersistsTriggerSource(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantEvents []string
	}{
		{
			name:       "task -> task.status_changed",
			source:     types.GoalTriggerSourceTask,
			wantEvents: []string{types.EventTaskStatusChanged},
		},
		{
			name:       "feature -> feature.completed",
			source:     types.GoalTriggerSourceFeature,
			wantEvents: []string{types.EventFeatureCompleted},
		},
		{
			name:       "both -> task.status_changed + feature.completed",
			source:     types.GoalTriggerSourceBoth,
			wantEvents: []string{types.EventTaskStatusChanged, types.EventFeatureCompleted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brain, store, _ := newTestBrainService(t)
			ctx := context.Background()

			const (
				project   = "proj"
				featureID = "feat-1"
				goalID    = "g1"
			)

			svc := NewGoalService(brain, &mockFeatureTaskLister{}, store)

			// Create a goal with a DIFFERENT trigger source than the target,
			// so the update genuinely changes it. Use "both" as the baseline,
			// except for the "both" case where we start from "task".
			startSource := types.GoalTriggerSourceBoth
			if tt.source == types.GoalTriggerSourceBoth {
				startSource = types.GoalTriggerSourceTask
			}

			if _, err := svc.CreateGoal(ctx, types.CreateGoalRequest{
				Project:   project,
				FeatureID: featureID,
				Title:     "Ship feature",
				Config: types.GoalConfig{
					ID:            goalID,
					Workdir:       "/tmp/goal-work",
					TriggerSource: startSource,
				},
				Action: *defaultGoalAction(),
			}); err != nil {
				t.Fatalf("CreateGoal: %v", err)
			}

			// Update the trigger source.
			ts := tt.source
			if _, err := svc.UpdateGoal(ctx, goalID, types.UpdateGoalRequest{
				TriggerSource: &ts,
			}); err != nil {
				t.Fatalf("UpdateGoal: %v", err)
			}

			// Re-read from storage (NOT the returned summary) to prove the
			// trigger_source config field was actually persisted.
			goals, err := svc.ListGoals(ctx, project, featureID)
			if err != nil {
				t.Fatalf("ListGoals: %v", err)
			}
			if len(goals) != 1 {
				t.Fatalf("ListGoals returned %d goals, want 1", len(goals))
			}
			got := goals[0]

			if got.Config == nil {
				t.Fatalf("persisted goal Config is nil")
			}
			if got.Config.TriggerSource != tt.source {
				t.Errorf("persisted Config.TriggerSource = %q, want %q",
					got.Config.TriggerSource, tt.source)
			}

			if got.Trigger == nil {
				t.Fatalf("persisted goal Trigger is nil")
			}
			gotEvents := got.Trigger.EventPatterns()
			if !reflect.DeepEqual(gotEvents, tt.wantEvents) {
				t.Errorf("persisted Trigger events = %v, want %v",
					gotEvents, tt.wantEvents)
			}
		})
	}
}

// =============================================================================
// HandleEvent dispatch tests (GA P2.3)
// =============================================================================

// saveFullGoalAutomation builds a canonical goal automation entry (trigger +
// goal config populated) via BuildGoalAutomation and persists it through
// brain.Save so it round-trips with both Trigger and Goal config from
// metadata. This is required to exercise HandleEvent's listing + matching.
func saveFullGoalAutomation(t *testing.T, brain *BrainServiceImpl, project, featureID, goalID, title string) {
	t.Helper()
	ctx := context.Background()

	entry, err := BuildGoalAutomation(GoalInput{
		Project:   project,
		FeatureID: featureID,
		Title:     title,
		Config:    types.GoalConfig{ID: goalID, Workdir: "/tmp/goal-work"},
		Action:    *defaultGoalAction(),
	})
	if err != nil {
		t.Fatalf("build goal automation: %v", err)
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
}

// TestHandleEvent_DispatchesReconcileForMatchingGoal verifies a goal-scoped
// task.status_changed event drives the reconcile, generating a need_work task.
func TestHandleEvent_DispatchesReconcileForMatchingGoal(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	saveFullGoalAutomation(t, brain, "proj", "feat-1", "g-dispatch", "Ship feature")

	// Empty lister => need_work => task generated when reconcile runs.
	svc := NewGoalService(brain, &mockFeatureTaskLister{}, store)

	err := svc.HandleEvent(ctx, types.Event{
		Type:      types.EventTaskStatusChanged,
		ID:        "evt_dispatch",
		ProjectID: "proj",
		FeatureID: "feat-1",
		ToStatus:  "completed",
	})
	if err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}

	tasks := listGeneratedGoalTasks(t, brain, "proj")
	if len(tasks) != 1 {
		t.Fatalf("generated task count = %d, want 1 (reconcile should have dispatched)", len(tasks))
	}

	audit := findGoalReconcileAudit(t, store)
	if audit.GoalID != "g-dispatch" {
		t.Errorf("audit GoalID = %q, want %q", audit.GoalID, "g-dispatch")
	}
	if audit.TriggeringEvent != types.EventTaskStatusChanged {
		t.Errorf("audit TriggeringEvent = %q, want %q", audit.TriggeringEvent, types.EventTaskStatusChanged)
	}
}

// TestHandleEvent_SkipsNonMatchingFeature verifies an event for a different
// feature does not trigger the goal's reconcile.
func TestHandleEvent_SkipsNonMatchingFeature(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	saveFullGoalAutomation(t, brain, "proj", "feat-1", "g-skip-feature", "Ship feature")
	svc := NewGoalService(brain, &mockFeatureTaskLister{}, store)

	// Event is for feat-2; the goal's trigger filters on feature_id=feat-1.
	err := svc.HandleEvent(ctx, types.Event{
		Type:      types.EventTaskStatusChanged,
		ProjectID: "proj",
		FeatureID: "feat-2",
		ToStatus:  "completed",
	})
	if err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}

	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 0 {
		t.Errorf("generated task count = %d, want 0 (non-matching feature must not reconcile)", len(tasks))
	}
}

// TestHandleEvent_SkipsNonMatchingEventType verifies an unrelated event type
// does not trigger the goal's reconcile.
func TestHandleEvent_SkipsNonMatchingEventType(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	// Task-only trigger source => only task.status_changed matches.
	entry, err := BuildGoalAutomation(GoalInput{
		Project:   "proj",
		FeatureID: "feat-1",
		Title:     "Ship feature",
		Config: types.GoalConfig{
			ID:            "g-skip-type",
			Workdir:       "/tmp/goal-work",
			TriggerSource: types.GoalTriggerSourceTask,
		},
		Action: *defaultGoalAction(),
	})
	if err != nil {
		t.Fatalf("build goal automation: %v", err)
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

	svc := NewGoalService(brain, &mockFeatureTaskLister{}, store)

	// feature.completed does not match a task-only trigger source.
	err = svc.HandleEvent(ctx, types.Event{
		Type:      types.EventFeatureCompleted,
		ProjectID: "proj",
		FeatureID: "feat-1",
	})
	if err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}

	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 0 {
		t.Errorf("generated task count = %d, want 0 (non-matching event type must not reconcile)", len(tasks))
	}
}

// TestHandleEvent_IgnoresNonGoalAutomations verifies a regular (non-goal)
// automation entry is not treated as a goal even if active.
func TestHandleEvent_IgnoresNonGoalAutomations(t *testing.T) {
	brain, store, _ := newTestBrainService(t)
	ctx := context.Background()

	// A plain active automation (no goal tag / generated_by) that would match
	// the event type but must be ignored by the goal handler.
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Plain automation",
		Content: "not a goal",
		Status:  "active",
		Project: "proj",
		Trigger: &types.TriggerConfig{Type: "event", Event: types.EventTaskStatusChanged},
		Action:  defaultGoalAction(),
	}); err != nil {
		t.Fatalf("save plain automation: %v", err)
	}

	svc := NewGoalService(brain, &mockFeatureTaskLister{}, store)

	err := svc.HandleEvent(ctx, types.Event{
		Type:      types.EventTaskStatusChanged,
		ProjectID: "proj",
		FeatureID: "feat-1",
	})
	if err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}

	if tasks := listGeneratedGoalTasks(t, brain, "proj"); len(tasks) != 0 {
		t.Errorf("generated task count = %d, want 0 (non-goal automation must be ignored)", len(tasks))
	}
}
