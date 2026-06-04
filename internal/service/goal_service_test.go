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
