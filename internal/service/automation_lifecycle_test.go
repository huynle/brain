package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// lifecycleSpec describes one project-owned automation for the lifecycle tests.
type lifecycleSpec struct {
	project   string
	trigger   string // "cron" (default) or "event"
	schedule  string
	startsAt  string
	expiresAt string
	maxRuns   *int
}

func saveLifecycleAutomation(t *testing.T, brain *BrainServiceImpl, spec lifecycleSpec) *types.CreateEntryResponse {
	t.Helper()
	trigger := &types.TriggerConfig{Type: "cron", Schedule: spec.schedule}
	if spec.trigger == "event" {
		trigger = &types.TriggerConfig{Type: "event", Event: "task.completed"}
	}
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:      "automation",
		Title:     "Lifecycle automation",
		Content:   "lifecycle fixture",
		Status:    "active",
		Project:   spec.project,
		Trigger:   trigger,
		Action:    &types.AutomationAction{Type: "prompt", DirectPrompt: "work on {{.Project}}"},
		StartsAt:  spec.startsAt,
		ExpiresAt: spec.expiresAt,
		MaxRuns:   spec.maxRuns,
	})
	if err != nil {
		t.Fatalf("Save automation: %v", err)
	}
	return resp
}

func lifecycleEntry(t *testing.T, brain *BrainServiceImpl, id string) types.BrainEntry {
	t.Helper()
	entry, err := brain.Recall(context.Background(), id)
	if err != nil {
		t.Fatalf("Recall(%s): %v", id, err)
	}
	return *entry
}

func TestAutomationLifecycle_NothingFiresBeforeStartsAt(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{
		project: "p", schedule: "* * * * *", startsAt: "2026-10-10T00:00:00Z",
	})

	svc := NewAutomationService(brain)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })
	stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
	if err := svc.CheckScheduled(ctx, now); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}

	if n := len(generatedTasksFor(t, brain, "p", auto.ID)); n != 0 {
		t.Fatalf("generated %d tasks before starts_at, want 0", n)
	}
}

// Preserved behaviour: once starts_at has passed, the cron still fires.
func TestAutomationLifecycle_FiresAfterStartsAt(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{
		project: "p", schedule: "* * * * *", startsAt: "2026-10-10T00:00:00Z",
	})

	svc := NewAutomationService(brain)
	now := time.Date(2026, 10, 10, 0, 1, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })
	stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
	if err := svc.CheckScheduled(ctx, now); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}

	if n := len(generatedTasksFor(t, brain, "p", auto.ID)); n != 1 {
		t.Fatalf("generated %d tasks after starts_at, want 1", n)
	}
}

func TestAutomationLifecycle_ExpiryCompletesOnceWithNote(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{
		project: "p", schedule: "* * * * *",
		startsAt: "2026-09-01T00:00:00Z", expiresAt: "2026-10-09T00:00:00Z",
	})

	svc := NewAutomationService(brain)
	now := time.Date(2026, 10, 9, 6, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })
	for i := 0; i < 2; i++ {
		stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
		if err := svc.CheckScheduled(ctx, now); err != nil {
			t.Fatalf("CheckScheduled sweep %d: %v", i, err)
		}
	}

	entry := lifecycleEntry(t, brain, auto.ID)
	if entry.Status != "completed" {
		t.Fatalf("status = %q, want completed", entry.Status)
	}
	const note = "Expired: expires_at passed (2026-10-09T00:00:00Z)"
	if n := strings.Count(entry.Content, note); n != 1 {
		t.Fatalf("expiry note appears %d times, want exactly 1:\n%s", n, entry.Content)
	}
	if n := len(generatedTasksFor(t, brain, "p", auto.ID)); n != 0 {
		t.Fatalf("expired automation generated %d tasks, want 0", n)
	}
}

// The fake clock alone decides expiry: real time is 2026-10-09, but the
// injected clock is 2027, so the automation must be treated as expired.
func TestAutomationLifecycle_ClockIsInjected(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{
		project: "p", schedule: "* * * * *", expiresAt: "2026-11-01T00:00:00Z",
	})

	svc := NewAutomationService(brain)
	fake := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return fake })
	stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
	if err := svc.CheckScheduled(ctx, fake); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}

	if got := lifecycleEntry(t, brain, auto.ID).Status; got != "completed" {
		t.Fatalf("status under fake clock 2027 = %q, want completed", got)
	}
}

// The expiry sweep covers every trigger type, not only cron.
func TestAutomationLifecycle_ExpirySweepCoversEveryTriggerType(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{
		project: "p", trigger: "event", expiresAt: "2026-10-09T00:00:00Z",
	})

	svc := NewAutomationService(brain)
	now := time.Date(2026, 10, 9, 6, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })
	stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
	if err := svc.CheckScheduled(ctx, now); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}

	if got := lifecycleEntry(t, brain, auto.ID).Status; got != "completed" {
		t.Fatalf("event automation status = %q, want completed", got)
	}
}

// A concurrent edit that moves expires_at into the future lands between our
// read and our write. The guarded write must lose the race and re-check, not
// overwrite the edit with a completion.
func TestAutomationLifecycle_ConcurrentEditIsNotClobbered(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{
		project: "p", schedule: "* * * * *", expiresAt: "2026-10-09T00:00:00Z",
	})

	svc := NewAutomationService(brain)
	now := time.Date(2026, 10, 9, 6, 0, 0, 0, time.UTC)
	const extended = "2026-12-31T00:00:00Z"
	attempts := 0
	err := svc.updateAutomationGuarded(ctx, auto.ID, func(current types.BrainEntry) *types.UpdateEntryRequest {
		attempts++
		if attempts == 1 {
			// Another writer extends the automation after we read it.
			if _, err := brain.Update(ctx, current.Path, types.UpdateEntryRequest{ExpiresAt: strPtr(extended)}); err != nil {
				t.Fatalf("concurrent edit: %v", err)
			}
		}
		return expiredCompletion(current, now)
	})
	if err != nil {
		t.Fatalf("updateAutomationGuarded: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("build ran %d times, want 2 (one lost race, one re-check)", attempts)
	}

	entry := lifecycleEntry(t, brain, auto.ID)
	if entry.Status != "active" {
		t.Fatalf("status = %q, want active: the concurrent extension must survive", entry.Status)
	}
	if entry.ExpiresAt != extended {
		t.Fatalf("expires_at = %q, want %q", entry.ExpiresAt, extended)
	}
	if strings.Contains(entry.Content, "Expired:") {
		t.Fatalf("expiry note written over a concurrent edit:\n%s", entry.Content)
	}
}

// If the entry keeps changing under us, the write is abandoned after one
// retry and the conflict is returned; nothing is forced.
func TestAutomationLifecycle_ConflictTwiceReturnsConflictAndLeavesEntry(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{project: "p", schedule: "* * * * *"})

	svc := NewAutomationService(brain)
	attempts := 0
	err := svc.updateAutomationGuarded(ctx, auto.ID, func(current types.BrainEntry) *types.UpdateEntryRequest {
		attempts++
		title := fmt.Sprintf("edited by writer %d", attempts)
		if _, uerr := brain.Update(ctx, current.Path, types.UpdateEntryRequest{Title: &title}); uerr != nil {
			t.Fatalf("concurrent edit %d: %v", attempts, uerr)
		}
		completed := "completed"
		return &types.UpdateEntryRequest{Status: &completed}
	})
	if !errors.Is(err, api.ErrConflict) {
		t.Fatalf("err = %v, want api.ErrConflict", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := lifecycleEntry(t, brain, auto.ID).Status; got != "active" {
		t.Fatalf("status = %q, want active", got)
	}
}

func generatedCount(t *testing.T, brain *BrainServiceImpl, project, automationID string) int {
	t.Helper()
	return len(generatedTasksFor(t, brain, project, automationID))
}

// max_runs counts runs that created work, per project-owned automation. Once
// the limit is reached the automation completes with the note.
func TestAutomationLifecycle_MaxRunsCompletesProjectOwnedAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	two := 2
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{project: "p", schedule: "* * * * *", maxRuns: &two})

	svc := NewAutomationService(brain)
	for i := 0; i < 3; i++ {
		now := time.Date(2026, 10, 9, 12, i, 0, 0, time.UTC)
		svc.SetClock(func() time.Time { return now })
		stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
		if err := svc.CheckScheduled(ctx, now); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}

	if n := generatedCount(t, brain, "p", auto.ID); n != 2 {
		t.Fatalf("generated %d tasks, want 2 (max_runs)", n)
	}
	entry := lifecycleEntry(t, brain, auto.ID)
	if entry.Status != "completed" {
		t.Fatalf("status = %q, want completed", entry.Status)
	}
	if n := strings.Count(entry.Content, "max_runs reached (2/2)"); n != 1 {
		t.Fatalf("max_runs note appears %d times, want 1:\n%s", n, entry.Content)
	}
}

// Skipped runs and manual runs do not count toward max_runs. Only the two
// work-creating cron fires do.
func TestAutomationLifecycle_MaxRunsIgnoresSkippedAndManualRuns(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	two := 2
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{project: "p", schedule: "* * * * *", maxRuns: &two})

	saveRunAuditForTest(t, brain, automationRunAudit{automation: *lifecycleEntryPtr(t, brain, auto.ID), project: "p", status: "skipped", skipReason: "paused"})
	saveRunAuditForTest(t, brain, automationRunAudit{automation: *lifecycleEntryPtr(t, brain, auto.ID), evt: types.Event{Type: "manual"}, project: "p", status: "queued"})

	svc := NewAutomationService(brain)
	for i := 0; i < 3; i++ {
		now := time.Date(2026, 10, 9, 13, i, 0, 0, time.UTC)
		svc.SetClock(func() time.Time { return now })
		stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
		if err := svc.CheckScheduled(ctx, now); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}

	if n := generatedCount(t, brain, "p", auto.ID); n != 2 {
		t.Fatalf("cron fires = %d, want 2: skipped and manual audits must not count", n)
	}
}

func lifecycleEntryPtr(t *testing.T, brain *BrainServiceImpl, id string) *types.BrainEntry {
	t.Helper()
	entry := lifecycleEntry(t, brain, id)
	return &entry
}

// Event-triggered automations are gated by max_runs too.
func TestAutomationLifecycle_MaxRunsGatesEventTrigger(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	one := 1
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{project: "p", trigger: "event", maxRuns: &one})

	svc := NewAutomationService(brain)
	now := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })
	evt := types.Event{Type: "task.completed", ProjectID: "p", Source: "service"}
	for i := 0; i < 2; i++ {
		if err := svc.HandleEvent(ctx, evt); err != nil {
			t.Fatalf("HandleEvent %d: %v", i, err)
		}
	}

	if n := generatedCount(t, brain, "p", auto.ID); n != 1 {
		t.Fatalf("event fires = %d, want 1", n)
	}
	if got := lifecycleEntry(t, brain, auto.ID).Status; got != "completed" {
		t.Fatalf("status = %q, want completed after max_runs", got)
	}
}

// Manual runs ignore lifecycle: a run before starts_at still creates a task,
// and it is tagged manual so it is never counted.
func TestAutomationLifecycle_ManualRunIgnoresLifecycleAndIsTagged(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	one := 1
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{
		project: "p", schedule: "* * * * *", startsAt: "2026-10-10T00:00:00Z", maxRuns: &one,
	})

	svc := NewAutomationService(brain)
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })
	if _, err := svc.RunAutomationNow(ctx, auto.ID, ""); err != nil {
		t.Fatalf("RunAutomationNow: %v", err)
	}
	if n := generatedCount(t, brain, "p", auto.ID); n != 1 {
		t.Fatalf("manual run created %d tasks before starts_at, want 1", n)
	}
	audits, err := svc.listRunAudits(ctx, "p", auto.ID, 10)
	if err != nil || len(audits) != 1 {
		t.Fatalf("listRunAudits = %d (err %v), want 1", len(audits), err)
	}
	if !hasRunAuditTag(audits[0].Tags, "manual") {
		t.Fatalf("manual run audit tags %v missing manual", audits[0].Tags)
	}

	// starts_at is still in the future, so this cron tick must not fire, even
	// though the manual run happened.
	stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
	if err := svc.CheckScheduled(ctx, time.Date(2026, 10, 9, 15, 1, 0, 0, time.UTC)); err != nil {
		t.Fatalf("CheckScheduled: %v", err)
	}
	if n := generatedCount(t, brain, "p", auto.ID); n != 1 {
		t.Fatalf("cron fired before starts_at after a manual run: %d tasks", n)
	}
}

// For a global automation, max_runs stops only the project that reached it.
// The first refusal writes one skip audit; later ticks stay silent.
func TestAutomationLifecycle_GlobalMaxRunsStopsOnlyThatProjectOnce(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	one := 1
	resp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Global capped", Content: "global capped", Status: "active",
		Global:  serviceBoolPtr(true),
		MaxRuns: &one,
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "* * * * *", Filter: map[string]string{"project": "*"}},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "work on {{.Project}}"},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Audits take their created time from types.TimeNowUTC. Pin it to the
	// fake tick time so each tick's audits are ordered as they would be in
	// production, instead of tying inside one wall-clock second.
	original := types.TimeNowUTC
	t.Cleanup(func() { types.TimeNowUTC = original })

	svc := NewAutomationService(brain)
	svc.SetProjectLister(&stubProjectLister{projects: []string{"a", "b"}})
	for i := 0; i < 3; i++ {
		now := time.Date(2026, 10, 9, 16, i, 0, 0, time.UTC)
		types.TimeNowUTC = func() time.Time { return now }
		svc.SetClock(func() time.Time { return now })
		stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
		if err := svc.CheckScheduled(ctx, now); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}

	if got := lifecycleEntry(t, brain, resp.ID).Status; got != "active" {
		t.Fatalf("global automation status = %q, want active (only projects stop)", got)
	}
	for _, project := range []string{"a", "b"} {
		if n := generatedCount(t, brain, project, resp.ID); n != 1 {
			t.Fatalf("project %s generated %d tasks, want 1", project, n)
		}
		audits, err := svc.listRunAudits(ctx, project, resp.ID, 50)
		if err != nil {
			t.Fatalf("listRunAudits(%s): %v", project, err)
		}
		skips := 0
		for _, a := range audits {
			if strings.Contains(a.Content, "skip_reason: max_runs\n") {
				skips++
			}
		}
		if skips != 1 {
			t.Fatalf("project %s: %d max_runs skip audits, want exactly 1", project, skips)
		}
	}
}

// max_runs of 0 or -1 means unlimited.
func TestAutomationLifecycle_MaxRunsZeroAndMinusOneAreUnlimited(t *testing.T) {
	for _, limit := range []int{0, -1} {
		brain, _, _ := newTestBrainService(t)
		ctx := context.Background()
		value := limit
		auto := saveLifecycleAutomation(t, brain, lifecycleSpec{project: "p", schedule: "* * * * *", maxRuns: &value})
		svc := NewAutomationService(brain)
		for i := 0; i < 3; i++ {
			now := time.Date(2026, 10, 9, 17, i, 0, 0, time.UTC)
			svc.SetClock(func() time.Time { return now })
			stampAutomationsModified(t, brain, slotUTC(2026, 10, 8, 0, 0, 0))
			if err := svc.CheckScheduled(ctx, now); err != nil {
				t.Fatalf("max_runs=%d tick %d: %v", limit, i, err)
			}
		}
		if n := generatedCount(t, brain, "p", auto.ID); n != 3 {
			t.Fatalf("max_runs=%d: generated %d, want 3", limit, n)
		}
	}
}
