package service

import (
	"context"
	"fmt"
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
		// Above the server's own 100-entry bulk cap on purpose: a test
		// that could only see the first page would pass while the tail
		// stayed un-archived.
		Limit: 1000,
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
	if _, err := svc.RunAutomationNow(ctx, saved.Path, ""); err == nil {
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
		if strings.Contains(r.Content, "skip_reason: no finished task to move") {
			skipped++
		}
	}
	if skipped == 0 {
		t.Fatal("the second pass left no skip on the record")
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

// THE RACE THIS EXISTS TO KILL. `feature.completed` fires ONE pass over
// the automation list. The built-in feature-checkout automation answers
// that same event by creating a PENDING checkout task stamped with the
// same feature id, and whichever automation the list yields first is
// decided by `modified DESC` — i.e. by which entry happened to be written
// last. If the archive ran over every status, it would archive the
// checkout task created microseconds earlier; an archived task never
// dispatches, so the merge would silently never happen, and the built-in's
// once_per dedup means it would never get a second task either.
func TestApplyUpdateAction_LeavesUnrunWorkAlone(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	seedFeatureTasks(t, brain, "shop")

	// Stand-in for the checkout task the same event just generated.
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Automation: checkout",
		Content:   "generated by the checkout automation for this very event",
		Status:    "pending",
		Project:   "shop",
		FeatureID: "auth",
	}); err != nil {
		t.Fatalf("seed generated task: %v", err)
	}

	svc := NewAutomationService(brain)
	if err := svc.applyUpdateAction(ctx, updateAutomation("shop", "archived"), types.Event{
		Type:      types.EventFeatureCompleted,
		ProjectID: "shop",
		FeatureID: "auth",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got := statusesByFeature(t, brain, "shop", "auth")
	var pending, archived int
	for _, s := range got {
		switch s {
		case "pending":
			pending++
		case "archived":
			archived++
		}
	}
	if pending != 1 {
		t.Fatalf("the un-run task was touched: %v", got)
	}
	if archived != 2 {
		t.Fatalf("the finished tasks were not archived: %v", got)
	}
}

func TestApplyUpdateAction_LeavesRunningWorkAlone(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	if _, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "still running",
		Content:   "x",
		Status:    "in_progress",
		Project:   "shop",
		FeatureID: "auth",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewAutomationService(brain)
	if err := svc.applyUpdateAction(ctx, updateAutomation("shop", "archived"), types.Event{
		Type:      types.EventFeatureCompleted,
		ProjectID: "shop",
		FeatureID: "auth",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := statusesByFeature(t, brain, "shop", "auth"); got[0] != "in_progress" {
		t.Fatalf("archived a task a runner is executing: %v", got)
	}
}

// A feature larger than one page must actually drain. It would not with a
// bare filter: the list is capped at 100 and ordered `modified DESC`, and
// writing a task bumps its mtime, so the rows just archived sort back to
// the front and the tail is never reached.
func TestApplyUpdateAction_DrainsAFeatureLargerThanOnePage(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	const n = 130
	for i := 0; i < n; i++ {
		if _, err := brain.Save(ctx, types.CreateEntryRequest{
			Type:      "task",
			Title:     fmt.Sprintf("t%03d", i),
			Content:   "x",
			Status:    "completed",
			Project:   "shop",
			FeatureID: "big",
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	svc := NewAutomationService(brain)
	if err := svc.applyUpdateAction(ctx, updateAutomation("shop", "archived"), types.Event{
		Type:      types.EventFeatureCompleted,
		ProjectID: "shop",
		FeatureID: "big",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got := statusesByFeature(t, brain, "shop", "big")
	if len(got) != n {
		t.Fatalf("expected %d tasks, listed %d", n, len(got))
	}
	for i, s := range got {
		if s != "archived" {
			t.Fatalf("task %d left at %q — the tail past the 100-cap never drained", i, s)
		}
	}
}

// A success must not be filed as a skip. Readers (the PWA's runOutcome
// among them) classify a run by looking for `error` then `skip_reason`,
// never at the entry status — so a success note in skip_reason renders an
// archive that moved three tasks as "skipped: updated 3/3 to archived",
// and a failure there hides behind the same glyph.
func TestApplyUpdateAction_AuditsSuccessAsSuccessNotSkip(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	seedFeatureTasks(t, brain, "shop")
	svc := NewAutomationService(brain)

	if err := svc.applyUpdateAction(ctx, updateAutomation("shop", "archived"), types.Event{
		Type:      types.EventFeatureCompleted,
		ProjectID: "shop",
		FeatureID: "auth",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	runs, err := brain.List(ctx, types.ListEntriesRequest{
		Project: "shop",
		Type:    "automation_run",
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	var found bool
	for _, r := range runs.Entries {
		if !strings.Contains(r.Content, "summary: updated 2/2 to archived") {
			continue
		}
		found = true
		if strings.Contains(r.Content, "skip_reason:") {
			t.Fatal("the success audit also carries a skip_reason — readers will call it a skip")
		}
		if strings.Contains(r.Content, "error:") {
			t.Fatal("the success audit carries an error line")
		}
	}
	if !found {
		t.Fatal("no success audit with a summary line")
	}
}
