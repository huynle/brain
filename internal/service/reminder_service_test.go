package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func newTestReminderService(t *testing.T, now time.Time) (*ReminderService, *BrainServiceImpl, *storage.StorageLayer) {
	t.Helper()
	brain, store, _ := newTestBrainService(t)
	svc := NewReminderService(brain, store, WithReminderClock(func() time.Time { return now }))
	return svc, brain, store
}

// The whole point of the type: an undated reminder is legitimate and must
// never fire on its own.
func TestReminder_UndatedNeverFires(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Come back to the indexer someday",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sum.State != types.ReminderStateUndated {
		t.Errorf("state = %q, want undated", sum.State)
	}
	if sum.Action != types.ReminderActionNotify {
		t.Errorf("action = %q, want the notify default", sum.Action)
	}

	fired, err := svc.SweepDue(ctx, now.Add(100*365*24*time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if fired != 0 {
		t.Errorf("an undated reminder fired (%d) — it has no time to be due at", fired)
	}
}

// A dated reminder fires once its time has passed, and reports as fired.
func TestReminder_DatedFiresAndBecomesTheNotification(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Stand-up",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T09:30:00Z"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sum.State != types.ReminderStateArmed {
		t.Fatalf("state = %q, want armed", sum.State)
	}

	// Before the time: nothing.
	if fired, err := svc.SweepDue(ctx, now); err != nil || fired != 0 {
		t.Fatalf("fired early: %d, err=%v", fired, err)
	}

	// After: exactly once.
	fired, err := svc.SweepDue(ctx, now.Add(31*time.Minute))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if fired != 1 {
		t.Fatalf("fired = %d, want 1", fired)
	}

	got, err := svc.GetReminder(ctx, sum.ReminderID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// status=pending IS the unacknowledged notification — no separate store.
	if got.State != types.ReminderStateFired {
		t.Errorf("state = %q, want fired", got.State)
	}
	if got.FiredAt == "" {
		t.Error("fired_at was not recorded")
	}
}

// Exactly-once across repeated sweeps, which is what the event-log dedup key
// buys. A reminder that re-notified every minute would be worse than useless.
func TestReminder_FiresExactlyOnceAcrossManySweeps(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	if _, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Once",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T09:00:00Z"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	total := 0
	for i := 0; i < 5; i++ {
		n, err := svc.SweepDue(ctx, now.Add(time.Duration(i+1)*time.Minute))
		if err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
		total += n
	}
	if total != 1 {
		t.Errorf("fired %d times across 5 sweeps, want exactly 1", total)
	}
}

// A reminder whose time passed while the server was down must still fire —
// late, once — rather than being skipped because its exact minute is gone.
// This is why the predicate is a threshold and not an equality.
func TestReminder_MissedWhileDownStillFires(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Missed while the box was down",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T09:05:00Z"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Three days later — the 09:05 minute is long gone.
	fired, err := svc.SweepDue(ctx, now.Add(72*time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if fired != 1 {
		t.Fatalf("fired = %d, want 1 — a missed reminder must fire late, not vanish", fired)
	}
	got, _ := svc.GetReminder(ctx, sum.ReminderID)
	if got.State != types.ReminderStateFired {
		t.Errorf("state = %q, want fired", got.State)
	}
}

// The task action generates a dispatchable task carrying the prompt.
func TestReminder_TaskActionGeneratesADispatchableTask(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, brain, _ := newTestReminderService(t, now)
	ctx := context.Background()

	if _, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Rotate the token",
		Config: types.ReminderConfig{
			RemindAt: "2026-09-10T09:00:00Z",
			Action:   types.ReminderActionTask,
			Prompt:   "Rotate the API token and update the deploy secret.",
		},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.SweepDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type: "task", Project: "brain", Limit: 50,
	})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var found *types.BrainEntry
	for i := range resp.Entries {
		if resp.Entries[i].GeneratedBy != "" &&
			len(resp.Entries[i].GeneratedBy) > 9 &&
			resp.Entries[i].GeneratedBy[:9] == "reminder:" {
			found = &resp.Entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no task generated; got %d tasks", len(resp.Entries))
	}
	// pending, or the scheduler will never dispatch it.
	if found.Status != "pending" {
		t.Errorf("task status = %q, want pending", found.Status)
	}
	// DirectPrompt is what the executor's prompt builder reads; Content alone
	// reaches the file body and nothing else.
	if found.DirectPrompt == "" {
		t.Error("task carries no direct_prompt — the agent would get an empty instruction")
	}
	// Origin must stay unset or every reminder task pins to the API host.
	if found.OriginMachineID != "" || found.MachineAffinity == "local" {
		t.Errorf("generated task carries origin provenance: machine=%q affinity=%q",
			found.OriginMachineID, found.MachineAffinity)
	}
}

// action=task without a prompt is refused at creation, not at fire time.
func TestReminder_TaskActionRequiresAPrompt(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	_, err := svc.CreateReminder(context.Background(), types.CreateReminderRequest{
		Project: "brain", Title: "No prompt",
		Config: types.ReminderConfig{
			RemindAt: "2026-09-10T09:00:00Z", Action: types.ReminderActionTask,
		},
	})
	if err == nil {
		t.Fatal("expected a refusal: action=task with no prompt is not work")
	}
}

// Snooze is its own verb because the one-shot claim is keyed on remind_at:
// only a NEW time can fire again.
func TestReminder_SnoozeRearmsAndFiresAgain(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Snoozy",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T09:00:00Z"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n, _ := svc.SweepDue(ctx, now.Add(time.Minute)); n != 1 {
		t.Fatalf("first fire = %d, want 1", n)
	}
	if _, err := svc.SnoozeReminder(ctx, sum.ReminderID, "2026-09-10T10:00:00Z"); err != nil {
		t.Fatalf("snooze: %v", err)
	}
	got, _ := svc.GetReminder(ctx, sum.ReminderID)
	if got.State != types.ReminderStateArmed {
		t.Fatalf("after snooze state = %q, want armed", got.State)
	}
	if n, _ := svc.SweepDue(ctx, now.Add(61*time.Minute)); n != 1 {
		t.Errorf("snoozed reminder did not fire again")
	}
}

// Clearing remind_at makes a dated reminder undated, which also has to drop
// the tag the sweeper lists by — otherwise it stays loaded forever.
func TestReminder_ClearingTheDateMakesItUndated(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Undate me",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T23:00:00Z"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	empty := ""
	got, err := svc.UpdateReminder(ctx, sum.ReminderID, types.UpdateReminderRequest{RemindAt: &empty})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.State != types.ReminderStateUndated {
		t.Errorf("state = %q, want undated", got.State)
	}
	if n, _ := svc.SweepDue(ctx, now.Add(48*time.Hour)); n != 0 {
		t.Errorf("an undated reminder still fired (%d)", n)
	}
}

// Lookup must work in every status, or a paused/fired reminder becomes
// unrecoverable — the exact trap the goal subsystem hit.
func TestReminder_LookupIsStatusAgnostic(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Findable",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T09:00:00Z"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.SweepDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetReminder(ctx, sum.ReminderID); err != nil {
		t.Errorf("fired reminder not findable: %v", err)
	}
	if _, err := svc.AckReminder(ctx, sum.ReminderID); err != nil {
		t.Errorf("ack failed: %v", err)
	}
	after, err := svc.GetReminder(ctx, sum.ReminderID)
	if err != nil {
		t.Fatalf("acked reminder not findable: %v", err)
	}
	if after.State != types.ReminderStateDone {
		t.Errorf("state = %q, want done", after.State)
	}
}
