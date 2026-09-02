package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// The update action is the only automation that writes directly — no
// runner, no prompt, no review in the way. Its entire safety story is
// that the scope comes from the EVENT and the write is refused when that
// scope is incomplete.
//
// That refusal is not defensive noise. An empty id in a bulk filter is
// not "match nothing": storage.ListOptions appends its WHERE clause only
// for a NON-EMPTY value, so a blank feature id reaches the database as no
// constraint at all, while every validation gate upstream still reports
// the filter as constrained. The bulk update would rewrite the first 100
// tasks of the project. These tests pin that door shut.

func seedFeatureTasks(t *testing.T, brain *BrainServiceImpl, project string) {
	t.Helper()
	ctx := context.Background()
	for _, spec := range []struct{ id, feature string }{
		{"t-auth-1", "auth"},
		{"t-auth-2", "auth"},
		{"t-other-1", "other"},
	} {
		_, err := brain.Save(ctx, types.CreateEntryRequest{
			Type:      "task",
			Title:     spec.id,
			Content:   "seed",
			Status:    "completed",
			Project:   project,
			FeatureID: spec.feature,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", spec.id, err)
		}
	}
}

func statusesByFeature(
	t *testing.T,
	brain *BrainServiceImpl,
	project, feature string,
) []string {
	t.Helper()
	out, err := brain.List(context.Background(), types.ListEntriesRequest{
		Project:   project,
		Type:      "task",
		FeatureID: feature,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := make([]string, 0, len(out.Entries))
	for _, e := range out.Entries {
		got = append(got, e.Status)
	}
	return got
}

func updateAutomation(project, status string) types.BrainEntry {
	return types.BrainEntry{
		ID:        "auto-archive",
		Type:      "automation",
		Title:     "Auto-archive completed features",
		Status:    "active",
		ProjectID: project,
		Trigger: &types.TriggerConfig{
			Type:    "event",
			Event:   types.EventFeatureCompleted,
			OncePer: "feature_id",
		},
		Action: &types.AutomationAction{
			Type:      types.AutomationActionUpdate,
			SetStatus: status,
		},
	}
}

func assertUntouched(t *testing.T, brain *BrainServiceImpl, project string) {
	t.Helper()
	for _, feature := range []string{"auth", "other"} {
		for _, got := range statusesByFeature(t, brain, project, feature) {
			if got != "completed" {
				t.Fatalf("feature %s was written to (status %q) — the refusal did not hold",
					feature, got)
			}
		}
	}
}

func TestApplyUpdateAction_RefusesAnEventWithNoFeature(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	seedFeatureTasks(t, brain, "shop")
	svc := NewAutomationService(brain)

	err := svc.applyUpdateAction(
		context.Background(),
		updateAutomation("shop", "archived"),
		// FeatureID deliberately empty — the unscoped case.
		types.Event{Type: types.EventFeatureCompleted, ProjectID: "shop"},
	)
	if err != nil {
		t.Fatalf("expected a clean skip, got %v", err)
	}
	assertUntouched(t, brain, "shop")
}

func TestApplyUpdateAction_RefusesWithNoProject(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	seedFeatureTasks(t, brain, "shop")
	svc := NewAutomationService(brain)

	err := svc.applyUpdateAction(
		context.Background(),
		updateAutomation("", "archived"),
		types.Event{Type: types.EventFeatureCompleted, FeatureID: "auth"},
	)
	if err != nil {
		t.Fatalf("expected a clean skip, got %v", err)
	}
	assertUntouched(t, brain, "shop")
}

func TestApplyUpdateAction_RefusesAnEmptyOrUnknownStatus(t *testing.T) {
	for _, status := range []string{"", "   ", "not-a-status"} {
		brain, _, _ := newTestBrainService(t)
		seedFeatureTasks(t, brain, "shop")
		svc := NewAutomationService(brain)

		err := svc.applyUpdateAction(
			context.Background(),
			updateAutomation("shop", status),
			types.Event{
				Type:      types.EventFeatureCompleted,
				ProjectID: "shop",
				FeatureID: "auth",
			},
		)
		if err != nil {
			t.Fatalf("status %q: expected a clean skip, got %v", status, err)
		}
		assertUntouched(t, brain, "shop")
	}
}

func TestApplyUpdateAction_ArchivesOnlyTheEventsFeature(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	seedFeatureTasks(t, brain, "shop")
	svc := NewAutomationService(brain)

	err := svc.applyUpdateAction(
		context.Background(),
		updateAutomation("shop", "archived"),
		types.Event{
			Type:      types.EventFeatureCompleted,
			ProjectID: "shop",
			FeatureID: "auth",
		},
	)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	for _, got := range statusesByFeature(t, brain, "shop", "auth") {
		if got != "archived" {
			t.Fatalf("auth task not archived: %q", got)
		}
	}
	// The sibling feature is the blast-radius check: a filter that lost
	// its feature pin would have taken this one too.
	for _, got := range statusesByFeature(t, brain, "shop", "other") {
		if got != "completed" {
			t.Fatalf("a task outside the event's feature was changed: %q", got)
		}
	}
}

// A manual run has no event, so there is nothing to scope the write to.
// Refusing beats guessing a target.
func TestRunAutomationNow_RefusesAnUpdateAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	seedFeatureTasks(t, brain, "shop")

	saved, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Auto-archive completed features",
		Content: "Archives a feature's tasks once it completes.",
		Status:  "active",
		Project: "shop",
		Trigger: &types.TriggerConfig{
			Type:    "event",
			Event:   types.EventFeatureCompleted,
			OncePer: "feature_id",
		},
		Action: &types.AutomationAction{
			Type:      types.AutomationActionUpdate,
			SetStatus: "archived",
		},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	svc := NewAutomationService(brain)
	if _, err := svc.RunAutomationNow(ctx, saved.Path); err == nil {
		t.Fatal("a manual run of an update automation must be refused")
	}
	assertUntouched(t, brain, "shop")
}

// The loop this guards against: archiving emits task.status_changed,
// CheckFeatureCompletion counts archived as done and re-emits
// feature.completed, and the automation fires again. Re-archiving an
// archived task is still a write, so without this the cycle never ends.
// The guard is "write only what is not already there", so the second pass
// changes nothing and therefore emits nothing.
func TestApplyUpdateAction_SecondPassWritesNothing(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	seedFeatureTasks(t, brain, "shop")
	svc := NewAutomationService(brain)
	evt := types.Event{
		Type:      types.EventFeatureCompleted,
		ProjectID: "shop",
		FeatureID: "auth",
	}

	if err := svc.applyUpdateAction(context.Background(), updateAutomation("shop", "archived"), evt); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	before := statusesByFeature(t, brain, "shop", "auth")

	// Second firing, same event — the case the loop would produce.
	if err := svc.applyUpdateAction(context.Background(), updateAutomation("shop", "archived"), evt); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	after := statusesByFeature(t, brain, "shop", "auth")

	if len(before) != len(after) {
		t.Fatalf("task count changed: %d -> %d", len(before), len(after))
	}
	for i := range after {
		if after[i] != "archived" {
			t.Fatalf("task %d is %q, not archived", i, after[i])
		}
	}

	// The audit is the observable proof the second pass was a no-op.
	runs, err := brain.List(context.Background(), types.ListEntriesRequest{
		Project: "shop",
		Type:    "automation_run",
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	var skipped int
	for _, r := range runs.Entries {
		if strings.Contains(r.Content, "already archived") {
			skipped++
		}
	}
	if skipped == 0 {
		t.Fatal("the second pass left no 'already archived' skip on the record")
	}
}

// A feature whose tasks only PARTLY need the change must still be
// completed by a later firing — the reason the guard is "skip when there
// is nothing to do" and not a fire-once dedup key.
func TestApplyUpdateAction_PicksUpTasksAddedAfterAnEarlierPass(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	seedFeatureTasks(t, brain, "shop")
	svc := NewAutomationService(brain)
	evt := types.Event{
		Type:      types.EventFeatureCompleted,
		ProjectID: "shop",
		FeatureID: "auth",
	}

	if err := svc.applyUpdateAction(ctx, updateAutomation("shop", "archived"), evt); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "t-auth-3",
		Content:   "added later",
		Status:    "completed",
		Project:   "shop",
		FeatureID: "auth",
	}); err != nil {
		t.Fatalf("save late task: %v", err)
	}

	if err := svc.applyUpdateAction(ctx, updateAutomation("shop", "archived"), evt); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	for _, got := range statusesByFeature(t, brain, "shop", "auth") {
		if got != "archived" {
			t.Fatalf("a task added between passes was left at %q", got)
		}
	}
}
