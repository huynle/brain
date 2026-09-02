package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/markdown"
)

// ReminderInput is what BuildReminderEntry turns into a storable entry.
type ReminderInput struct {
	Project   string
	FeatureID string
	Title     string
	Content   string
	Global    bool
	Tags      []string
	Config    types.ReminderConfig
}

// BuildReminderEntry validates a reminder and produces the entry to save.
//
// Kept pure and separate from the service so the validation rules can be
// tested without a store, and so the MCP/REST/PWA front doors cannot disagree
// about what a valid reminder is.
func BuildReminderEntry(in ReminderInput) (types.CreateEntryRequest, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return types.CreateEntryRequest{}, fmt.Errorf("title is required")
	}
	if !in.Global && strings.TrimSpace(in.Project) == "" {
		return types.CreateEntryRequest{}, fmt.Errorf(
			"project is required (or set global:true) — a reminder filed under " +
				"the wrong project is one nobody will see")
	}

	cfg := in.Config
	cfg.ID = strings.TrimSpace(cfg.ID)
	if cfg.ID == "" {
		cfg.ID = markdown.GenerateShortID()
	}
	cfg.RemindAt = strings.TrimSpace(cfg.RemindAt)
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)

	action := types.NormalizeReminderAction(cfg.Action)
	if action == "" {
		return types.CreateEntryRequest{}, fmt.Errorf(
			"unknown reminder action %q (want one of: %s)",
			in.Config.Action, strings.Join(types.ReminderActions, ", "))
	}
	cfg.Action = action

	// A firing time must parse, and must carry an explicit offset. There is no
	// server-wide timezone anywhere in this codebase, so a naive wall-clock
	// string has nothing to resolve against and would fire at the wrong hour
	// on any machine that is not in the author's zone.
	if cfg.RemindAt != "" {
		t, err := time.Parse(time.RFC3339, cfg.RemindAt)
		if err != nil {
			return types.CreateEntryRequest{}, fmt.Errorf(
				"remind_at %q is not RFC3339 with an offset "+
					"(e.g. 2026-09-10T09:00:00-06:00 or 2026-09-10T15:00:00Z): %w",
				cfg.RemindAt, err)
		}
		// Re-emit in a canonical form so the dedup key is stable regardless of
		// how the caller spelled the same instant.
		cfg.RemindAt = t.Format(time.RFC3339)
	}

	// A task-action reminder with no prompt is not work, it is noise — and it
	// would reach an agent as an empty instruction.
	if action == types.ReminderActionTask && strings.TrimSpace(cfg.Prompt) == "" {
		return types.CreateEntryRequest{}, fmt.Errorf(
			"action %q requires a prompt: it is the instruction the generated task carries",
			types.ReminderActionTask)
	}

	tags := append([]string{}, in.Tags...)
	tags = appendMissingTag(tags, types.ReminderTag)
	tags = appendMissingTag(tags, types.ReminderIDTag(cfg.ID))
	// The dated tag is load-bearing, not decorative: the sweeper LISTS by it,
	// so an undated reminder is never loaded at all rather than being loaded
	// and skipped by a predicate.
	if cfg.RemindAt != "" {
		tags = appendMissingTag(tags, types.ReminderDatedTag)
	}

	global := in.Global
	req := types.CreateEntryRequest{
		Type:      "reminder",
		Title:     title,
		Content:   reminderContent(in.Content, title),
		Status:    "active",
		Project:   strings.TrimSpace(in.Project),
		FeatureID: strings.TrimSpace(in.FeatureID),
		Tags:      tags,
		Reminder:  &cfg,
	}
	if global {
		req.Global = &global
	}
	return req, nil
}

// reminderContent guarantees a non-empty body: POST /entries rejects an empty
// one, and "the title again" is a truthful default for a one-line reminder.
func reminderContent(content, title string) string {
	if strings.TrimSpace(content) != "" {
		return content
	}
	return title
}

func appendMissingTag(tags []string, want string) []string {
	for _, t := range tags {
		if t == want {
			return tags
		}
	}
	return append(tags, want)
}

// ReminderStateFor derives the lifecycle state reported by the API, so no
// caller has to re-derive it from status plus the presence of a date.
func ReminderStateFor(status string, cfg *types.ReminderConfig) string {
	switch status {
	case "completed", "validated":
		return types.ReminderStateDone
	case "pending":
		return types.ReminderStateFired
	case "blocked", "cancelled", "archived":
		return types.ReminderStatePaused
	}
	if cfg.IsDated() {
		return types.ReminderStateArmed
	}
	return types.ReminderStateUndated
}

// ReminderSummaryFrom projects an entry into the API view.
func ReminderSummaryFrom(e types.BrainEntry) types.ReminderSummary {
	cfg := e.Reminder
	if cfg == nil {
		cfg = &types.ReminderConfig{}
	}
	return types.ReminderSummary{
		EntryID:         e.ID,
		ReminderID:      cfg.ID,
		Title:           e.Title,
		Project:         e.ProjectID,
		FeatureID:       e.FeatureID,
		Path:            e.Path,
		Content:         e.Content,
		Status:          e.Status,
		State:           ReminderStateFor(e.Status, cfg),
		RemindAt:        cfg.RemindAt,
		Timezone:        cfg.Timezone,
		Action:          cfg.NormalizedAction(),
		Prompt:          cfg.Prompt,
		FiredAt:         cfg.FiredAt,
		GeneratedTaskID: cfg.GeneratedTaskID,
	}
}
