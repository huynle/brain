package service

import (
	"context"
	"log/slog"
	"strconv"
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
//
// trigger.skip_if_event and trigger.only_if_event gate a slot by the events of
// an ics source; see eventGateAllows for their rules and data availability.

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

// dayFiltersFor returns the day filters that gate an automation's slots. Each
// filter is present only when its trigger field is set: trigger.calendar,
// trigger.skip_if_event and trigger.only_if_event. A slot runs only on a day
// that every filter allows.
func (s *AutomationService) dayFiltersFor(automation types.BrainEntry) []schedule.DayFilter {
	if automation.Trigger == nil {
		return nil
	}
	var filters []schedule.DayFilter
	registry := s.calendarRegistry()
	if name := automation.Trigger.Calendar; name != "" {
		filters = append(filters, calendarDayFilter(automation, name, registry))
	}
	if f := automation.Trigger.SkipIfEvent; f != nil {
		filters = append(filters, eventDayFilter(automation, "trigger.skip_if_event", registry, *f, false))
	}
	if f := automation.Trigger.OnlyIfEvent; f != nil {
		filters = append(filters, eventDayFilter(automation, "trigger.only_if_event", registry, *f, true))
	}
	return filters
}

// hasDayFilters reports whether a trigger sets any day filter. The timeline
// uses it to decide which projections need the scheduler's day filters.
func hasDayFilters(tc *types.TriggerConfig) bool {
	return tc != nil && (tc.Calendar != "" || tc.SkipIfEvent != nil || tc.OnlyIfEvent != nil)
}

// calendarDayFilter is the trigger.calendar gate: a day is allowed when the
// named builtin day calendar is open. Any other name fails closed.
func calendarDayFilter(automation types.BrainEntry, name string, registry *calendar.Registry) schedule.DayFilter {
	day, ok := registry.DayCalendar(name)
	if !ok {
		warnCalendarGateOnce(automation, name, registry == nil)
		return closedEveryDay
	}
	return schedule.DayFilterFunc(func(_ context.Context, base time.Time) (bool, error) {
		return day.IsOpen(marketcal.DateOf(base)), nil
	})
}

// eventDayFilter is the gate for one event filter: skip_if_event when onlyIf is
// false, only_if_event when it is true. The source is read when a day is
// evaluated, not when the schedule is compiled, so each poll is seen.
func eventDayFilter(automation types.BrainEntry, field string, registry *calendar.Registry, f types.CalendarEventFilter, onlyIf bool) schedule.DayFilter {
	return schedule.DayFilterFunc(func(_ context.Context, day time.Time) (bool, error) {
		return eventGateAllows(automation, field, registry, f, onlyIf, day), nil
	})
}

// eventGateAllows decides one local day for an event filter.
//
// A day "has a matching event" when some occurrence of f.Calendar covers the
// day and matches every set field of f (eventFilterMatches). skip_if_event
// allows a day with no match; only_if_event allows a day with a match.
//
// Data availability. The source's last good snapshot is used even when it is
// stale; the poller raises the stale alert on its own. When the source has no
// snapshot at all, or the day is outside the snapshot's window (Window), the
// event cannot be confirmed either way. Then skip_if_event allows the day (fail
// open: the slot runs) and only_if_event does not (fail closed). Each such case
// warns once per automation modification and reason (failEventGate).
func eventGateAllows(automation types.BrainEntry, field string, registry *calendar.Registry, f types.CalendarEventFilter, onlyIf bool, day time.Time) bool {
	occurrences, status, known := registry.Occurrences(f.Calendar)
	if !known || status.LastSuccess.IsZero() {
		return failEventGate(automation, field, f.Calendar, onlyIf, "no snapshot yet")
	}
	start, end, ok := registry.Window(f.Calendar)
	if !ok || !dayOverlaps(day, start, end) {
		return failEventGate(automation, field, f.Calendar, onlyIf, "day outside the snapshot window")
	}
	matched := false
	for _, o := range occurrences {
		if eventCoversDay(o, day) && eventFilterMatches(o, f) {
			matched = true
			break
		}
	}
	if onlyIf {
		return matched
	}
	return !matched
}

// failEventGate applies the fail-open or fail-closed rule for a day the source
// cannot answer, and warns once per automation modification and reason.
func failEventGate(automation types.BrainEntry, field, name string, onlyIf bool, reason string) bool {
	allowed := !onlyIf
	key := "event\x00" + automation.ID + "\x00" + automation.Modified + "\x00" + field + "\x00" + reason
	if _, loaded := calendarWarnings.LoadOrStore(key, struct{}{}); !loaded {
		effect := "the slot does not fire"
		if allowed {
			effect = "the slot fires"
		}
		slog.Warn("automation event gate has no usable calendar data for this day; "+effect,
			"automation", automation.ID, "field", field, "calendar", name, "reason", reason)
	}
	return allowed
}

// eventFilterMatches reports whether an occurrence matches every set field of
// f, using the shared filter-value forms. Unset fields are ignored, so a filter
// that names only a calendar matches any occurrence. all_day is compared as
// "true" or "false".
func eventFilterMatches(o calendar.Occurrence, f types.CalendarEventFilter) bool {
	if f.Title != "" && !types.MatchFilterValue(o.Title, f.Title) {
		return false
	}
	if f.Description != "" && !types.MatchFilterValue(o.Description, f.Description) {
		return false
	}
	if f.Location != "" && !types.MatchFilterValue(o.Location, f.Location) {
		return false
	}
	if f.AllDay != "" && !types.MatchFilterValue(strconv.FormatBool(o.AllDay), f.AllDay) {
		return false
	}
	return true
}

// dayBounds returns the local day of day: its midnight and the following
// midnight, in day's location. The day filter contract says only day.Date()
// matters, and day is local midnight in the schedule's location.
func dayBounds(day time.Time) (time.Time, time.Time) {
	loc := day.Location()
	y, m, d := day.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc), time.Date(y, m, d+1, 0, 0, 0, 0, loc)
}

// dayOverlaps reports whether the local day of day shares any time with the
// half-open span [start, end).
func dayOverlaps(day, start, end time.Time) bool {
	dayStart, dayEnd := dayBounds(day)
	return dayStart.Before(end) && start.Before(dayEnd)
}

// eventCoversDay reports whether an occurrence covers the local day of day.
//
// A timed occurrence covers the day when it overlaps the local day, and so
// counts on every day of a multi-day event. Its end is exclusive, so an event
// ending exactly at midnight does not cover the next day. A zero-length
// occurrence covers the day holding its start instant.
//
// An all-day occurrence covers each calendar date from its start date up to,
// but not including, its end date. Start and End are local midnights in the
// feed's default location, so those dates are read in that location and
// compared as calendar dates, never converted to another zone. An all-day
// occurrence whose end is not after its start covers its start date only.
func eventCoversDay(o calendar.Occurrence, day time.Time) bool {
	if o.AllDay {
		return allDayCoversDate(o, day)
	}
	dayStart, dayEnd := dayBounds(day)
	if !o.End.After(o.Start) {
		return !o.Start.Before(dayStart) && o.Start.Before(dayEnd)
	}
	return o.Start.Before(dayEnd) && o.End.After(dayStart)
}

// allDayCoversDate reports whether the all-day occurrence covers the calendar
// date of day. Dates are compared as naive dates, so no zone conversion happens.
func allDayCoversDate(o calendar.Occurrence, day time.Time) bool {
	first := civilDate(o.Start)
	last := civilDate(o.End)
	if !last.After(first) {
		last = first.AddDate(0, 0, 1)
	}
	target := civilDate(day)
	return !target.Before(first) && target.Before(last)
}

// civilDate returns the calendar date of t in t's own location, as a UTC
// midnight. Dates built this way compare without any zone conversion.
func civilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// closedEveryDay is the fail-closed filter: no day is allowed, so no slot exists.
var closedEveryDay = schedule.DayFilterFunc(func(context.Context, time.Time) (bool, error) {
	return false, nil
})

// calendarWarnings records which automation modifications have already logged
// a fail-closed calendar warning, keyed by ID and modification stamp. Event
// gate warnings share it under their own key prefix.
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
