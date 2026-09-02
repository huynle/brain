package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// CreateReminder validates and stores a new reminder.
func (s *ReminderService) CreateReminder(ctx context.Context, req types.CreateReminderRequest) (*types.ReminderSummary, error) {
	global := req.Global != nil && *req.Global
	entryReq, err := BuildReminderEntry(ReminderInput{
		Project:   req.Project,
		FeatureID: req.FeatureID,
		Title:     req.Title,
		Content:   req.Content,
		Global:    global,
		Tags:      req.Tags,
		Config:    req.Config,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.brain.Save(ctx, entryReq); err != nil {
		return nil, fmt.Errorf("save reminder: %w", err)
	}
	return s.GetReminder(ctx, entryReq.Reminder.ID)
}

// ListReminders returns reminders, optionally scoped by project and state.
func (s *ReminderService) ListReminders(ctx context.Context, project, state string) ([]types.ReminderSummary, error) {
	project = strings.TrimSpace(project)
	// Pushed into SQL rather than filtered in Go: ListEntriesRequest.Project
	// reaches a WHERE clause, so scoping here is the difference between
	// reading one project's reminders and reading every project's.
	entries, err := s.listReminderEntries(ctx, "", project)
	if err != nil {
		return nil, err
	}
	state = strings.TrimSpace(strings.ToLower(state))

	out := make([]types.ReminderSummary, 0, len(entries))
	for _, e := range entries {
		// An entry typed `reminder` with no reminder block is not a reminder
		// this service made — it would list with an empty id, which no verb
		// can then address.
		if e.Reminder == nil {
			continue
		}
		sum := ReminderSummaryFrom(e)
		if state != "" && sum.State != state {
			continue
		}
		out = append(out, sum)
	}
	return out, nil
}

// GetReminder looks a reminder up by its reminder id.
func (s *ReminderService) GetReminder(ctx context.Context, reminderID string) (*types.ReminderSummary, error) {
	e, err := s.findReminderByID(ctx, reminderID)
	if err != nil {
		return nil, err
	}
	sum := ReminderSummaryFrom(*e)
	return &sum, nil
}

// UpdateReminder patches a reminder. Every field is presence-based: a pointer
// to "" CLEARS the value, which is how a dated reminder is made undated.
func (s *ReminderService) UpdateReminder(ctx context.Context, reminderID string, req types.UpdateReminderRequest) (*types.ReminderSummary, error) {
	e, err := s.findReminderByID(ctx, reminderID)
	if err != nil {
		return nil, err
	}
	cfg := *e.Reminder

	if req.RemindAt != nil {
		v := strings.TrimSpace(*req.RemindAt)
		if v != "" {
			t, perr := time.Parse(time.RFC3339, v)
			if perr != nil {
				return nil, fmt.Errorf(
					"remind_at %q is not RFC3339 with an offset: %w", v, perr)
			}
			v = t.Format(time.RFC3339)
		}
		cfg.RemindAt = v
	}
	if req.Timezone != nil {
		cfg.Timezone = strings.TrimSpace(*req.Timezone)
	}
	if req.Repeat != nil {
		v := types.NormalizeReminderRepeat(*req.Repeat)
		if v == "!" {
			return nil, fmt.Errorf("unknown repeat %q (want one of: %s)",
				*req.Repeat, strings.Join(types.ReminderRepeats, ", "))
		}
		cfg.Repeat = v
	}
	if req.RepeatUntil != nil {
		v := strings.TrimSpace(*req.RepeatUntil)
		if v != "" {
			t, perr := time.Parse(time.RFC3339, v)
			if perr != nil {
				return nil, fmt.Errorf("repeat_until %q is not RFC3339 with an offset: %w", v, perr)
			}
			v = t.Format(time.RFC3339)
		}
		cfg.RepeatUntil = v
	}
	if req.Action != nil {
		a := types.NormalizeReminderAction(*req.Action)
		if a == "" {
			return nil, fmt.Errorf("unknown reminder action %q (want one of: %s)",
				*req.Action, strings.Join(types.ReminderActions, ", "))
		}
		cfg.Action = a
	}
	if req.Prompt != nil {
		cfg.Prompt = *req.Prompt
	}
	if req.Agent != nil {
		cfg.Agent = strings.TrimSpace(*req.Agent)
	}
	if req.Model != nil {
		cfg.Model = strings.TrimSpace(*req.Model)
	}
	if req.Executor != nil {
		cfg.Executor = strings.TrimSpace(*req.Executor)
	}
	if req.ExecutionMode != nil {
		cfg.ExecutionMode = strings.TrimSpace(*req.ExecutionMode)
	}
	if req.TargetWorkdir != nil {
		cfg.TargetWorkdir = strings.TrimSpace(*req.TargetWorkdir)
	}

	if cfg.NormalizedAction() == types.ReminderActionTask &&
		strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("action %q requires a prompt", types.ReminderActionTask)
	}
	if cfg.Repeats() && cfg.RemindAt == "" {
		return nil, fmt.Errorf("repeat %q requires a remind_at to recur from", cfg.Repeat)
	}

	if req.Status != nil {
		v := strings.TrimSpace(*req.Status)
		// Free-form status was a way to strand a reminder: the sweeper only
		// loads active/pending, so any other value silently removed it from
		// the schedule while every endpoint still reported it happily.
		if !types.IsValidEntryStatus(v) {
			return nil, fmt.Errorf("unknown status %q", v)
		}
	}

	update := types.UpdateEntryRequest{Reminder: &cfg}
	if req.Title != nil {
		update.Title = req.Title
	}
	if req.Content != nil {
		update.Content = req.Content
	}
	if req.Status != nil {
		update.Status = req.Status
	}
	// The dated tag decides whether the sweeper ever loads this entry, so it
	// has to track remind_at on every edit — not just at creation.
	update.Tags = reminderTagsFor(e.Tags, cfg)

	if _, err := s.brain.Update(ctx, e.Path, update); err != nil {
		return nil, fmt.Errorf("update reminder: %w", err)
	}
	return s.GetReminder(ctx, reminderID)
}

// AckReminder dismisses a fired reminder.
//
// For a RECURRING reminder this means "dismiss this occurrence", not "stop
// reminding me" — so it goes back to active, still armed for its next
// occurrence, which the firing already scheduled. Marking it completed would
// silently cancel the recurrence, and the user asked to be reminded again.
// Stopping a recurrence is an explicit edit (clear repeat) or a delete.
func (s *ReminderService) AckReminder(ctx context.Context, reminderID string) (*types.ReminderSummary, error) {
	e, err := s.findReminderByID(ctx, reminderID)
	if err != nil {
		return nil, err
	}
	next := "completed"
	if e.Reminder.Repeats() && e.Reminder.IsDated() {
		next = "active"
	}
	return s.UpdateReminder(ctx, reminderID, types.UpdateReminderRequest{Status: &next})
}

// SnoozeReminder re-arms a reminder for a new time.
//
// It is its own verb rather than "set status back to active" because the
// exactly-once claim is keyed on remind_at: only a NEW time can fire again.
// Reactivating without moving the time would look like it worked and then
// never fire, which is the worst of both.
func (s *ReminderService) SnoozeReminder(ctx context.Context, reminderID, until string) (*types.ReminderSummary, error) {
	until = strings.TrimSpace(until)
	if until == "" {
		return nil, fmt.Errorf("snooze requires a new remind_at")
	}
	active := "active"
	return s.UpdateReminder(ctx, reminderID, types.UpdateReminderRequest{
		Status:   &active,
		RemindAt: &until,
	})
}

// DeleteReminder removes a reminder entirely.
func (s *ReminderService) DeleteReminder(ctx context.Context, reminderID string) error {
	e, err := s.findReminderByID(ctx, reminderID)
	if err != nil {
		return err
	}
	return s.brain.Delete(ctx, e.Path)
}

// FireReminderNow fires a reminder immediately, ignoring its schedule.
func (s *ReminderService) FireReminderNow(ctx context.Context, reminderID string) (*types.ReminderSummary, error) {
	e, err := s.findReminderByID(ctx, reminderID)
	if err != nil {
		return nil, err
	}
	if e.Reminder == nil {
		return nil, fmt.Errorf("reminder %s has no config", reminderID)
	}
	now := s.now()
	at := now
	if t, err := e.Reminder.RemindAtTime(); err == nil {
		at = t
	}
	// A unique suffix, so a manual fire is its own claim rather than
	// colliding with the scheduled firing's and silently doing nothing.
	suffix := fmt.Sprintf("manual:%d", now.UTC().UnixNano())
	if _, err := s.fire(ctx, *e, at, now, suffix); err != nil {
		return nil, err
	}
	return s.GetReminder(ctx, reminderID)
}

// findReminderByID is deliberately STATUS-AGNOSTIC.
//
// The goal subsystem learned this the hard way: an active-only lookup made a
// paused goal unrecoverable, because every endpoint that could un-pause it
// 404'd first. Only the sweeper filters to active.
func (s *ReminderService) findReminderByID(ctx context.Context, reminderID string) (*types.BrainEntry, error) {
	reminderID = strings.TrimSpace(reminderID)
	if reminderID == "" {
		return nil, types.ErrReminderNotFound
	}
	entries, err := s.listReminderEntries(ctx, types.ReminderIDTag(reminderID), "")
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Reminder != nil && entries[i].Reminder.ID == reminderID {
			return &entries[i], nil
		}
	}
	return nil, types.ErrReminderNotFound
}

// listReminderEntries lists reminder entries in every status, optionally
// filtered to one tag.
func (s *ReminderService) listReminderEntries(ctx context.Context, tag, project string) ([]types.BrainEntry, error) {
	var out []types.BrainEntry
	const page = 200
	offset := 0
	for {
		resp, err := s.brain.List(ctx, types.ListEntriesRequest{
			Type:    "reminder",
			Tags:    tag,
			Project: project,
			Limit:   page,
			Offset:  offset,
		})
		if err != nil {
			return nil, fmt.Errorf("list reminders: %w", err)
		}
		if resp == nil || len(resp.Entries) == 0 {
			return out, nil
		}
		out = append(out, resp.Entries...)
		if len(resp.Entries) < page {
			return out, nil
		}
		offset += page
		if offset > 100_000 {
			return out, nil
		}
	}
}

// reminderTagsFor recomputes the tag set so reminder:dated tracks remind_at.
func reminderTagsFor(existing []string, cfg types.ReminderConfig) []string {
	out := make([]string, 0, len(existing)+1)
	for _, t := range existing {
		if t == types.ReminderDatedTag {
			continue // re-added below only if still dated
		}
		out = append(out, t)
	}
	out = appendMissingTag(out, types.ReminderTag)
	out = appendMissingTag(out, types.ReminderIDTag(cfg.ID))
	if cfg.IsDated() {
		out = appendMissingTag(out, types.ReminderDatedTag)
	}
	return out
}
