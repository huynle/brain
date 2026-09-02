package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// reminderEventIngester is the narrow slice of EventServiceImpl the reminder
// sweeper needs. Declared locally so the service can be constructed in tests
// without an event pipeline.
type reminderEventIngester interface {
	Ingest(ctx context.Context, events []types.Event) error
}

// reminderSweepInterval is how often the sweeper looks for due reminders.
//
// One minute, not the goal loop's five: a 09:00 reminder that surfaces at
// 09:04 has failed at the one thing a reminder does. AutomationService.Start
// already proves a 1m tick is affordable at this scale.
//
// Package-level so tests can shrink it.
var reminderSweepInterval = time.Minute

// ReminderService fires dated reminders.
//
// It runs in the API process, NOT the runner. Two reasons, both decisive:
// a notify-action reminder has nothing to do with runners and must not go
// undelivered because none happens to be polling; and the runner's existing
// run_once_at machinery is additionally gated by the project and feature pause
// dials, which have no business suppressing a notification.
type ReminderService struct {
	brain  *BrainServiceImpl
	store  *storage.StorageLayer
	events reminderEventIngester

	// pauseChecker gates the TASK action only — see fireOne. The per-project
	// variant is a distinct interface from the global automationPauseChecker.
	pauseChecker automationProjectPauseChecker

	now func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// ReminderServiceOption configures a ReminderService.
type ReminderServiceOption func(*ReminderService)

// WithReminderPauseChecker supplies the project automations pause dial.
func WithReminderPauseChecker(c automationProjectPauseChecker) ReminderServiceOption {
	return func(s *ReminderService) { s.pauseChecker = c }
}

// WithReminderEventIngester supplies the event sink for reminder.fired.
func WithReminderEventIngester(e reminderEventIngester) ReminderServiceOption {
	return func(s *ReminderService) { s.events = e }
}

// WithReminderClock overrides the clock, for tests.
func WithReminderClock(now func() time.Time) ReminderServiceOption {
	return func(s *ReminderService) { s.now = now }
}

// NewReminderService builds the service. store may be nil only in tests that
// never fire; the dedup claim needs it.
func NewReminderService(brain *BrainServiceImpl, store *storage.StorageLayer, opts ...ReminderServiceOption) *ReminderService {
	s := &ReminderService{
		brain: brain,
		store: store,
		now:   time.Now,
		locks: make(map[string]*sync.Mutex),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Start runs the sweep loop until ctx is cancelled.
//
// It sweeps ONCE before entering the loop. GoalService.Start does not, and so
// waits a full interval after a restart; a reminder service coming back from
// an outage must drain what it missed immediately, not a minute later.
func (s *ReminderService) Start(ctx context.Context) {
	if s == nil || s.brain == nil {
		return
	}
	slog.Info("reminder sweeper started", "interval", reminderSweepInterval)
	defer slog.Info("reminder sweeper stopped")

	if fired, err := s.SweepDue(ctx, s.now()); err != nil {
		slog.Warn("reminder sweep failed", "error", err, "fired", fired)
	}

	ticker := time.NewTicker(reminderSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if fired, err := s.SweepDue(ctx, s.now()); err != nil {
				slog.Warn("reminder sweep failed", "error", err, "fired", fired)
			}
		}
	}
}

// SweepDue fires every armed reminder whose time has passed.
//
// Per-reminder failures are logged and the pass CONTINUES, returning the first
// error. Abandoning the pass on the first failure would let one malformed
// reminder block every other one behind it, indefinitely.
func (s *ReminderService) SweepDue(ctx context.Context, now time.Time) (int, error) {
	entries, err := s.listArmed(ctx)
	if err != nil {
		return 0, err
	}
	var fired int
	var firstErr error
	for _, e := range entries {
		if e.Reminder == nil || !e.Reminder.IsDated() {
			continue
		}
		if !reminderIsArmed(e) {
			continue
		}
		at, err := e.Reminder.RemindAtTime()
		if err != nil {
			// A reminder whose date does not parse can never fire. Say so
			// once per sweep rather than silently skipping it forever.
			slog.Warn("reminder has an unparseable remind_at",
				"entry", e.ID, "remind_at", e.Reminder.RemindAt, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// A THRESHOLD, never an exact-minute match. Exact matching (which the
		// cron path uses) permanently loses any firing whose minute the
		// process happened to miss — a restart, a slow tick, a laptop asleep.
		// With a threshold, drift only ever delays.
		if now.UTC().Before(at.UTC()) {
			continue
		}
		payload, err := s.fireOne(ctx, e, at, now)
		if err != nil {
			slog.Warn("reminder failed to fire", "entry", e.ID, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// A nil payload means nothing happened — suppressed by the pause dial,
		// or already recorded. Counting those as fired made the sweep report
		// work it verifiably did not do.
		if payload != nil {
			fired++
		}
	}
	return fired, firstErr
}

// reminderIsArmed reports whether an entry is eligible to fire.
//
// `active` is the ordinary armed state. `pending` means "fired and not yet
// acknowledged" — eligible ONLY when the reminder repeats, which is what lets
// a daily reminder keep firing while yesterday's notification is still on
// screen. Everything else (completed, blocked, cancelled, archived) is a
// deliberate stop.
func reminderIsArmed(e types.BrainEntry) bool {
	switch e.Status {
	case "active":
		return true
	case "pending":
		return e.Reminder.Repeats()
	default:
		return false
	}
}

// listArmed loads dated reminders. Undated ones are excluded by the
// TAG filter rather than by a predicate, so they are never loaded at all.
func (s *ReminderService) listArmed(ctx context.Context) ([]types.BrainEntry, error) {
	// NOTE: no Status filter.
	//
	// A recurring reminder sits at `pending` between occurrences — that is how
	// its unacknowledged notification stays visible — so filtering to `active`
	// would fire it once and never again. Status is applied per entry below
	// instead: `active` is armed, `pending` is armed only when it repeats, and
	// completed/blocked/cancelled/archived are never due.
	var out []types.BrainEntry
	const page = 200
	offset := 0
	for {
		resp, err := s.brain.List(ctx, types.ListEntriesRequest{
			Type:   "reminder",
			Tags:   types.ReminderDatedTag,
			Limit:  page,
			Offset: offset,
		})
		if err != nil {
			return nil, fmt.Errorf("list armed reminders: %w", err)
		}
		if resp == nil || len(resp.Entries) == 0 {
			return out, nil
		}
		out = append(out, resp.Entries...)
		if len(resp.Entries) < page {
			return out, nil
		}
		offset += page
		// Both existing precedents hard-code Limit:1000 and silently drop the
		// tail; paging here means a user with 1001 reminders still gets the
		// 1001st.
		if offset > 100_000 {
			return out, nil
		}
	}
}

// fireOne performs one reminder's firing: claim, act, record.
func (s *ReminderService) fireOne(ctx context.Context, e types.BrainEntry, at, now time.Time) (*types.ReminderFiredPayload, error) {
	return s.fire(ctx, e, at, now, "")
}

// fire performs one firing. keySuffix distinguishes firings that share a
// remind_at: the scheduled one passes "", a manual "fire now" passes a unique
// value, because otherwise every manual fire of the same reminder collides
// with the scheduled firing's claim and does nothing.
func (s *ReminderService) fire(ctx context.Context, e types.BrainEntry, at, now time.Time, keySuffix string) (*types.ReminderFiredPayload, error) {
	cfg := e.Reminder
	lock := s.lockReminder(cfg.ID)
	lock.Lock()
	defer lock.Unlock()

	action := cfg.NormalizedAction()

	// The TASK action answers to the project automations dial; the NOTIFY
	// action does not. The dial is about generating work. Suppressing an
	// in-app notification would not defer it — the claim below is one-shot,
	// so a suppressed notification is a lost one.
	if action == types.ReminderActionTask && s.pauseChecker != nil &&
		e.ProjectID != "" && s.pauseChecker.IsAutomationsPausedForProject(e.ProjectID) {
		// Deliberately no claim row: the reminder stays armed and fires when
		// the project is resumed.
		return nil, nil
	}

	// The claim. A UNIQUE partial index on event_log(dedup_key) makes this the
	// only atomic, restart-proof, re-index-proof one-shot primitive in the
	// tree: a second insert with the same key ERRORS rather than duplicating.
	// The key includes remind_at so a snooze (which rewrites it) can fire
	// again while a mere reactivation cannot.
	dedupKey := fmt.Sprintf("reminder:%s:%s", cfg.ID, cfg.RemindAt)
	if keySuffix != "" {
		dedupKey += ":" + keySuffix
	}

	lateBy := int64(now.UTC().Sub(at.UTC()) / time.Second)
	if lateBy < 0 {
		lateBy = 0
	}
	payload := &types.ReminderFiredPayload{
		ReminderID:    cfg.ID,
		EntryID:       e.ID,
		Project:       e.ProjectID,
		Title:         e.Title,
		Action:        action,
		RemindAt:      cfg.RemindAt,
		FiredAt:       now.UTC().Format(time.RFC3339),
		LateBySeconds: lateBy,
	}

	claimed := true
	if s.store != nil {
		blob, _ := json.Marshal(payload)
		if _, err := s.store.InsertEvent(ctx, types.EventReminderFired,
			string(blob), dedupKey, "reminder"); err != nil {
			if !isDuplicateDedupKey(err) {
				return nil, fmt.Errorf("claim reminder %s: %w", cfg.ID, err)
			}
			// Someone already claimed this (id, remind_at). What that means is
			// NOT knowable from the status — "crashed mid-firing" and "was
			// reactivated without moving the time" both leave it active — so
			// decide from durable evidence on the entry itself.
			claimed = false
		}
	}

	// The claim row is written BEFORE the action runs, so a crash between the
	// two leaves a claim for an action that never happened. `fired_at` and
	// `generated_task_id` are the durable record of what actually completed,
	// and they are what the heal path reads.
	alreadyRecorded := strings.TrimSpace(cfg.FiredAt) != ""
	taskOutstanding := action == types.ReminderActionTask &&
		strings.TrimSpace(cfg.GeneratedTaskID) == ""

	if !claimed && alreadyRecorded && !taskOutstanding {
		// This firing genuinely happened. Do nothing at all — in particular
		// do NOT re-stamp fired_at and flip the status, which would fake a
		// fresh notification for a reminder somebody merely reactivated.
		return nil, nil
	}

	if action == types.ReminderActionTask && (claimed || taskOutstanding) {
		taskID, err := s.actionTask(ctx, e)
		if err != nil {
			return nil, fmt.Errorf("reminder %s task action: %w", cfg.ID, err)
		}
		payload.GeneratedTaskID = taskID
	}

	// Record the outcome on the entry. `status` is the durable half — it
	// reaches the markdown file, so a fired reminder survives a restart and a
	// re-index. status=pending IS the unacknowledged notification.
	next := *cfg
	next.FiredAt = payload.FiredAt
	next.FireCount = cfg.FireCount + 1
	if payload.GeneratedTaskID != "" {
		next.GeneratedTaskID = payload.GeneratedTaskID
	}

	// A recurring reminder advances to its next occurrence NOW, in the same
	// write that records this firing. It stays at `pending` so the
	// unacknowledged notification remains visible — acknowledging it is what
	// returns it to `armed`.
	//
	// The next time is computed from the SCHEDULED time, not from `now`, so a
	// 09:00 daily reminder stays at 09:00 instead of walking later every time
	// the sweeper ticks a few seconds late. It is advanced until it is in the
	// future, which is what makes an outage collapse into one catch-up firing
	// rather than a burst of backdated ones.
	if cfg.Repeats() {
		at := at
		for {
			n, ok := cfg.NextOccurrence(at)
			if !ok {
				break
			}
			at = n
			if at.After(now) {
				break
			}
		}
		if cfg.RepeatEnded(at) {
			// The recurrence is over: leave the date where it was so the
			// history reads truthfully, and drop the dated tag by clearing it.
			next.RemindAt = ""
		} else {
			next.RemindAt = at.Format(time.RFC3339)
			payload.NextRemindAt = next.RemindAt
		}
	}

	firedStatus := "pending"
	update := types.UpdateEntryRequest{
		Status:   &firedStatus,
		Reminder: &next,
	}
	// The dated tag decides whether the sweeper ever loads this entry again,
	// so an ended recurrence has to drop it here too.
	update.Tags = reminderTagsFor(e.Tags, next)
	if _, err := s.brain.Update(ctx, e.Path, update); err != nil {
		return nil, fmt.Errorf("record reminder %s as fired: %w", cfg.ID, err)
	}

	if (claimed || taskOutstanding) && s.events != nil {
		// Through the event service so automations and webhooks can trigger
		// off reminder.fired with no new engine code. Metadata is what
		// TriggerConfig.Filter matches on, so the discriminating fields go
		// there rather than only into an opaque payload.
		if err := s.events.Ingest(ctx, []types.Event{{
			Type: types.EventReminderFired,
			// MUST be one of the two sources EventServiceImpl.Ingest accepts
			// ("runner" or "api"); it rejects anything else BEFORE publishing.
			// A bare "reminder" here made every single firing fail to reach
			// the hub — no webhook, no event automation, no SSE — with only a
			// WARN line, while the README advertised the opposite.
			Source:    types.EventSourceAPI,
			Timestamp: now.UTC(),
			ProjectID: e.ProjectID,
			TaskID:    payload.GeneratedTaskID,
			Metadata: map[string]string{
				"reminder_id":     payload.ReminderID,
				"entry_id":        payload.EntryID,
				"title":           payload.Title,
				"action":          payload.Action,
				"remind_at":       payload.RemindAt,
				"fired_at":        payload.FiredAt,
				"late_by_seconds": fmt.Sprintf("%d", payload.LateBySeconds),
			},
		}}); err != nil {
			slog.Warn("reminder.fired event not ingested", "entry", e.ID, "error", err)
		}
	}
	return payload, nil
}

// lockReminder serializes concurrent firings of the same reminder (the ticker
// racing a manual "fire now").
func (s *ReminderService) lockReminder(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.locks[id]; ok {
		return m
	}
	m := &sync.Mutex{}
	s.locks[id] = m
	return m
}

// isDuplicateDedupKey reports whether err is the unique-index violation that
// means "someone already claimed this firing".
func isDuplicateDedupKey(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint failed")
}
