package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/marketcal"
	"github.com/huynle/brain-api/pkg/schedule"
)

// Calendar day filters for clock triggers.
//
// trigger.calendar names a builtin day calendar (config server.calendars). A
// slot exists only on a day that calendar reports open, judged by the slot's
// local date in the trigger's timezone (schedule.DayFilter contract). Anything
// else fails closed: a name that is unknown, an ics source (not a day
// calendar), or no registry at all yields a filter that closes every day, so
// no slot fires, and a warning is logged once per automation modification.

// SetCalendars installs the calendar registry the day filters and save-time
// validation read. It is nil-safe and is meant to be called once while the
// server is wired, before any tick or write. The registry is the one the
// validator uses too (see BrainServiceImpl.SetCalendars).
func (s *AutomationService) SetCalendars(reg *calendar.Registry) {
	if s == nil || s.brain == nil {
		return
	}
	s.brain.SetCalendars(reg)
}

// SetCalendars installs the calendar registry. Nil-safe. A nil registry means
// no calendar is configured, so every calendar gate fails closed.
func (s *BrainServiceImpl) SetCalendars(reg *calendar.Registry) {
	if s == nil {
		return
	}
	s.calendars = reg
}

// calendarRegistry returns the installed registry, or nil when none is.
func (s *AutomationService) calendarRegistry() *calendar.Registry {
	if s == nil || s.brain == nil {
		return nil
	}
	return s.brain.calendars
}

// automationDayFilterSource is what the timeline needs from the scheduler to
// gate its projections exactly as the scheduler gates its slots.
type automationDayFilterSource interface {
	dayFiltersFor(automation types.BrainEntry) []schedule.DayFilter
}

// dayFiltersFor returns the day filters that gate an automation's slots: one
// for trigger.calendar, and none when the trigger names no calendar. The
// result is never empty for a named calendar, so an unusable name cannot leak
// through as "no filter".
func (s *AutomationService) dayFiltersFor(automation types.BrainEntry) []schedule.DayFilter {
	if automation.Trigger == nil || automation.Trigger.Calendar == "" {
		return nil
	}
	name := automation.Trigger.Calendar
	registry := s.calendarRegistry()
	day, ok := registry.DayCalendar(name)
	if !ok {
		warnCalendarGateOnce(automation, name, registry == nil)
		return []schedule.DayFilter{closedEveryDay}
	}
	return []schedule.DayFilter{schedule.DayFilterFunc(func(_ context.Context, base time.Time) (bool, error) {
		return day.IsOpen(marketcal.DateOf(base)), nil
	})}
}

// closedEveryDay is the fail-closed filter: no day is allowed, so no slot exists.
var closedEveryDay = schedule.DayFilterFunc(func(context.Context, time.Time) (bool, error) {
	return false, nil
})

// calendarWarnings records which automation modifications have already logged
// a fail-closed calendar warning, keyed by ID and modification stamp.
var calendarWarnings sync.Map

// warnCalendarGateOnce logs why a calendar gate fails closed, once per
// automation modification. The calendar name is config-defined, not a secret.
func warnCalendarGateOnce(automation types.BrainEntry, name string, noRegistry bool) {
	key := automation.ID + "\x00" + automation.Modified
	if _, loaded := calendarWarnings.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	reason := "not a configured builtin calendar"
	if noRegistry {
		reason = "no calendars are configured"
	}
	slog.Warn("automation calendar gate fails closed: no slot will fire",
		"automation", automation.ID, "calendar", name, "reason", reason)
}
