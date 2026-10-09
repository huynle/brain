package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/schedule"
)

// automationScheduleSpec returns the pkg/schedule Spec for a cron-triggered
// automation entry. It is the one translation from trigger fields to a
// schedule: the scheduler and the timeline both build their schedules here,
// so they cannot disagree about when an automation fires.
//
// The Spec carries no project. Compile it, then take each target's stagger
// offset from Schedule.Offset(entry.ID, project) and pass it to NextSlot or
// LatestSlot.
//
// Anchor is set only for an every trigger: starts_at when present, else the
// entry's created instant. Compile ignores the anchor for cron.
//
// Calendar day filters (trigger.calendar, skip_if_event, only_if_event) are
// not translated yet, so the Spec alone does not describe a calendar-gated
// automation.
func automationScheduleSpec(entry types.BrainEntry) (schedule.Spec, error) {
	trigger := entry.Trigger
	if trigger == nil {
		return schedule.Spec{}, errors.New("automation has no trigger")
	}
	spec := schedule.Spec{
		Cron:     trigger.Schedule,
		Every:    trigger.Every,
		At:       trigger.At,
		Timezone: trigger.Timezone,
	}
	if trigger.Stagger != "" {
		stagger, err := time.ParseDuration(trigger.Stagger)
		if err != nil {
			return schedule.Spec{}, fmt.Errorf("stagger %q: %w", trigger.Stagger, err)
		}
		spec.Stagger = stagger
	}
	if trigger.Every != "" {
		anchor, err := automationAnchor(entry)
		if err != nil {
			return schedule.Spec{}, err
		}
		spec.Anchor = anchor
	}
	return spec, nil
}

// automationAnchor is where an every schedule starts: starts_at, else the
// created instant. Both are RFC 3339 strings.
func automationAnchor(entry types.BrainEntry) (time.Time, error) {
	value, field := entry.StartsAt, "starts_at"
	if value == "" {
		value, field = entry.Created, "created"
	}
	if value == "" {
		return time.Time{}, errors.New("every needs starts_at or a created instant to anchor it")
	}
	anchor, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s anchor %q: %w", field, value, err)
	}
	return anchor, nil
}
