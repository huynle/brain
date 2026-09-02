package service

import (
	"context"
	"fmt"
	"strings"
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

// A weekly reminder must keep firing. The first version stopped after one
// firing because the sweeper filtered to status=active and a fired reminder
// sits at pending.
func TestReminder_WeeklyKeepsFiring(t *testing.T) {
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC) // a Monday
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Weekly review",
		Config: types.ReminderConfig{
			RemindAt: "2026-09-07T09:00:00Z",
			Repeat:   types.ReminderRepeatWeekly,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Four consecutive weeks, sweeping just after each occurrence.
	for week := 0; week < 4; week++ {
		at := now.Add(time.Duration(week)*7*24*time.Hour + time.Minute)
		n, err := svc.SweepDue(ctx, at)
		if err != nil {
			t.Fatalf("week %d sweep: %v", week, err)
		}
		if n != 1 {
			t.Fatalf("week %d fired %d times, want 1", week, n)
		}
	}

	got, err := svc.GetReminder(ctx, sum.ReminderID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FireCount != 4 {
		t.Errorf("fire_count = %d, want 4", got.FireCount)
	}
	// And it is still scheduled for the fifth week, not exhausted.
	if got.RemindAt == "" {
		t.Error("recurring reminder lost its next occurrence")
	}
}

// The next occurrence is computed from the SCHEDULED time, not from the
// firing instant, or a daily 09:00 reminder walks later every day.
func TestReminder_DailyDoesNotDriftLater(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Daily standup",
		Config: types.ReminderConfig{
			RemindAt: "2026-09-10T09:00:00Z", Repeat: types.ReminderRepeatDaily,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Sweep 47 seconds late each day — the drift a naive "now + 24h" would
	// accumulate into a visibly wrong time within a couple of weeks.
	for day := 0; day < 5; day++ {
		at := now.Add(time.Duration(day)*24*time.Hour + 47*time.Second)
		if _, err := svc.SweepDue(ctx, at); err != nil {
			t.Fatalf("day %d: %v", day, err)
		}
	}
	got, _ := svc.GetReminder(ctx, sum.ReminderID)
	parsed, err := time.Parse(time.RFC3339, got.RemindAt)
	if err != nil {
		t.Fatalf("next remind_at unparseable: %v", err)
	}
	if parsed.Hour() != 9 || parsed.Minute() != 0 || parsed.Second() != 0 {
		t.Errorf("drifted to %s — recurrence must advance from the scheduled time", got.RemindAt)
	}
}

// An outage must collapse into ONE catch-up firing, not a burst of backdated
// ones — the reason NextOccurrence is advanced in a loop until it is future.
func TestReminder_OutageCollapsesToOneCatchUp(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Daily during an outage",
		Config: types.ReminderConfig{
			RemindAt: "2026-09-10T09:00:00Z", Repeat: types.ReminderRepeatDaily,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Nothing swept for ten days.
	n, err := svc.SweepDue(ctx, now.Add(10*24*time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("fired %d times after a 10-day outage, want 1 catch-up", n)
	}
	got, _ := svc.GetReminder(ctx, sum.ReminderID)
	next, _ := time.Parse(time.RFC3339, got.RemindAt)
	if !next.After(now.Add(10 * 24 * time.Hour)) {
		t.Errorf("next occurrence %s is not in the future — it would fire again immediately", got.RemindAt)
	}
}

// Acknowledging a recurring reminder dismisses THIS occurrence; it must not
// cancel the recurrence, which is what marking it completed would do.
func TestReminder_AckOnRecurringKeepsItArmed(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Recurring",
		Config: types.ReminderConfig{
			RemindAt: "2026-09-10T09:00:00Z", Repeat: types.ReminderRepeatDaily,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.SweepDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	acked, err := svc.AckReminder(ctx, sum.ReminderID)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if acked.State != types.ReminderStateArmed {
		t.Errorf("state after ack = %q, want armed — ack dismisses the occurrence, not the series", acked.State)
	}
	// And it still fires tomorrow.
	if n, _ := svc.SweepDue(ctx, now.Add(25*time.Hour)); n != 1 {
		t.Error("acknowledged recurring reminder stopped firing")
	}
}

// A one-shot reminder's ack still completes it.
func TestReminder_AckOnOneShotCompletesIt(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, _ := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "One shot",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T09:00:00Z"},
	})
	if _, err := svc.SweepDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	acked, err := svc.AckReminder(ctx, sum.ReminderID)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if acked.State != types.ReminderStateDone {
		t.Errorf("state = %q, want done", acked.State)
	}
}

// repeat_until stops the series.
func TestReminder_RepeatUntilEndsTheSeries(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Bounded",
		Config: types.ReminderConfig{
			RemindAt:    "2026-09-10T09:00:00Z",
			Repeat:      types.ReminderRepeatDaily,
			RepeatUntil: "2026-09-12T09:00:00Z",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	total := 0
	for day := 0; day < 6; day++ {
		n, _ := svc.SweepDue(ctx, now.Add(time.Duration(day)*24*time.Hour+time.Minute))
		total += n
	}
	if total != 3 {
		t.Errorf("fired %d times, want 3 (the 10th, 11th and 12th)", total)
	}
	got, _ := svc.GetReminder(ctx, sum.ReminderID)
	if got.RemindAt != "" {
		t.Errorf("series should be exhausted, still scheduled for %s", got.RemindAt)
	}
}

// A repeat with no date would look scheduled and never fire.
func TestReminder_RepeatRequiresADate(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	_, err := svc.CreateReminder(context.Background(), types.CreateReminderRequest{
		Project: "brain", Title: "Recurring nothing",
		Config: types.ReminderConfig{Repeat: types.ReminderRepeatWeekly},
	})
	if err == nil {
		t.Fatal("expected a refusal: a recurrence needs a start")
	}
}

// An unknown repeat must be refused, not silently downgraded to one-shot —
// a reminder that was meant to recur and does not is a silent failure.
func TestReminder_UnknownRepeatIsRefused(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	_, err := svc.CreateReminder(context.Background(), types.CreateReminderRequest{
		Project: "brain", Title: "Bad cadence",
		Config: types.ReminderConfig{
			RemindAt: "2026-09-10T09:00:00Z", Repeat: "fortnightly",
		},
	})
	if err == nil {
		t.Fatal("expected a refusal for an unknown repeat")
	}
}

// The reminder.fired event has to actually reach the hub. It did not: the
// source was the bare string "reminder", and EventServiceImpl.Ingest rejects
// anything but "runner"/"api" BEFORE publishing — so every firing failed
// silently with a WARN line while the README advertised webhook and
// automation integration that could never work.
type recordingIngester struct{ got []types.Event }

func (r *recordingIngester) Ingest(_ context.Context, events []types.Event) error {
	// Mirror EventServiceImpl's own gate, so this test fails the same way
	// production did rather than accepting anything.
	for i, e := range events {
		if e.Source != types.EventSourceRunner && e.Source != types.EventSourceAPI {
			return fmt.Errorf("invalid event source %q at index %d", e.Source, i)
		}
	}
	r.got = append(r.got, events...)
	return nil
}

func TestReminder_FiredEventUsesAnAcceptedSource(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	brain, store, _ := newTestBrainService(t)
	rec := &recordingIngester{}
	svc := NewReminderService(brain, store,
		WithReminderClock(func() time.Time { return now }),
		WithReminderEventIngester(rec),
	)
	ctx := context.Background()

	if _, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Emits an event",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T09:00:00Z"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.SweepDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(rec.got) != 1 {
		t.Fatalf("got %d events, want 1 — the firing never reached the hub", len(rec.got))
	}
	if rec.got[0].Type != types.EventReminderFired {
		t.Errorf("event type = %q", rec.got[0].Type)
	}
	if rec.got[0].Metadata["reminder_id"] == "" {
		t.Error("event carries no reminder_id, so no automation filter can match it")
	}
}

// A crash between the claim and the task action must not lose the action.
// The claim row is written first, so a naive "duplicate key means already
// done" inference silently dropped the task forever.
func TestReminder_HealRunsATaskActionTheClaimOutlived(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, brain, store := newTestReminderService(t, now)
	ctx := context.Background()

	sum, err := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Crashed mid-fire",
		Config: types.ReminderConfig{
			RemindAt: "2026-09-10T09:00:00Z",
			Action:   types.ReminderActionTask,
			Prompt:   "do the thing",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate the crash: the claim row exists, nothing else happened.
	if _, err := store.InsertEvent(ctx, types.EventReminderFired, "{}",
		"reminder:"+sum.ReminderID+":2026-09-10T09:00:00Z", "reminder"); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	if _, err := svc.SweepDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type: "task", Project: "brain", Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range resp.Entries {
		if strings.HasPrefix(e.GeneratedBy, "reminder:") {
			found = true
		}
	}
	if !found {
		t.Error("the task action was lost: a claim row outlived a crash and suppressed it forever")
	}
}

// A reminder merely reactivated (status back to active, same time) must NOT
// fake a fresh notification — its claim is already spent.
func TestReminder_ReactivationWithoutANewTimeDoesNotRefire(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestReminderService(t, now)
	ctx := context.Background()

	sum, _ := svc.CreateReminder(ctx, types.CreateReminderRequest{
		Project: "brain", Title: "Reactivated",
		Config: types.ReminderConfig{RemindAt: "2026-09-10T09:00:00Z"},
	})
	if _, err := svc.SweepDue(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	first, _ := svc.GetReminder(ctx, sum.ReminderID)

	active := "active"
	if _, err := svc.UpdateReminder(ctx, sum.ReminderID,
		types.UpdateReminderRequest{Status: &active}); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	n, err := svc.SweepDue(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("fired %d times on a spent claim — reactivating is not snoozing", n)
	}
	after, _ := svc.GetReminder(ctx, sum.ReminderID)
	if after.FiredAt != first.FiredAt {
		t.Errorf("fired_at was re-stamped (%s -> %s), faking a firing that did not happen",
			first.FiredAt, after.FiredAt)
	}
}
