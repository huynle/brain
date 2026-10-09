package service

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/schedule"
)

// Slot-based scheduling for cron-triggered automations.
//
// A schedule yields slots (pkg/schedule). An automation fires once per slot
// per target project. This file holds the trigger-to-schedule translation and
// the per-modification compile cache; the evaluation itself lives in
// CheckScheduled.

// compiledAutomationSchedule is one automation trigger compiled for one
// modification of its entry. err is set, and sched nil, when the trigger
// cannot be scheduled; such an entry is skipped.
type compiledAutomationSchedule struct {
	modified string
	sched    *schedule.Schedule
	catchUp  schedule.CatchUp
	err      error
}

// automationScheduleSpec translates an automation's trigger into a
// schedule.Spec. Day filters are attached by the caller (dayFiltersFor).
func automationScheduleSpec(automation types.BrainEntry) (schedule.Spec, error) {
	if automation.Trigger == nil {
		return schedule.Spec{}, errors.New("automation has no trigger")
	}
	tc := automation.Trigger
	spec := schedule.Spec{
		Cron:     tc.Schedule,
		Every:    tc.Every,
		At:       tc.At,
		Timezone: tc.Timezone,
		Anchor:   automationAnchor(automation),
	}
	if tc.Stagger != "" {
		stagger, err := time.ParseDuration(tc.Stagger)
		if err != nil {
			return schedule.Spec{}, fmt.Errorf("trigger.stagger: %w", err)
		}
		spec.Stagger = stagger
	}
	return spec, nil
}

// automationAnchor is where an interval schedule starts: starts_at when it is
// set and parses, else the entry's creation instant. The zero time means the
// anchor is missing, which Compile rejects for an interval.
func automationAnchor(automation types.BrainEntry) time.Time {
	if automation.StartsAt != "" {
		if start, err := time.Parse(time.RFC3339, automation.StartsAt); err == nil {
			return start
		}
	}
	created, _ := time.Parse(time.RFC3339, automation.Created)
	return created
}

// dayFiltersFor returns the day filters that gate an automation's slots. The
// built-in calendar and event filters arrive in a later change, so none apply
// yet and every slot exists.
func (s *AutomationService) dayFiltersFor(automation types.BrainEntry) []schedule.DayFilter {
	return nil
}

// compiledScheduleFor returns automation's compiled schedule, compiling it
// again only when the entry was modified since it was last compiled. ok is
// false when the trigger cannot be scheduled. A warning is logged once per
// modification, not on every tick.
func (s *AutomationService) compiledScheduleFor(automation types.BrainEntry) (*compiledAutomationSchedule, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if cached, ok := s.compiled[automation.ID]; ok && cached.modified == automation.Modified {
		return cached, cached.err == nil
	}
	compiled := s.compileAutomationSchedule(automation)
	if compiled.err != nil {
		slog.Warn("automation schedule skipped: trigger cannot be scheduled",
			"automation", automation.ID, "error", compiled.err)
	}
	if s.compiled == nil {
		s.compiled = make(map[string]*compiledAutomationSchedule)
	}
	s.compiled[automation.ID] = compiled
	return compiled, compiled.err == nil
}

func (s *AutomationService) compileAutomationSchedule(automation types.BrainEntry) *compiledAutomationSchedule {
	compiled := &compiledAutomationSchedule{modified: automation.Modified}
	spec, err := automationScheduleSpec(automation)
	if err != nil {
		compiled.err = err
		return compiled
	}
	spec.DayFilters = s.dayFiltersFor(automation)
	sched, err := schedule.Compile(spec)
	if err != nil {
		compiled.err = err
		return compiled
	}
	catchUp, err := schedule.ParseCatchUp(automation.Trigger.CatchUp)
	if err != nil {
		compiled.err = fmt.Errorf("trigger.catch_up: %w", err)
		return compiled
	}
	compiled.sched = sched
	compiled.catchUp = catchUp
	return compiled
}
