package service

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// validateAutomation is the check every automation write runs at save time: the
// scheduling rules, then the calendar names against the installed registry.
// It is the single hook the create, update and metadata paths share.
func (s *BrainServiceImpl) validateAutomation(ctx context.Context, fm *frontmatter.Frontmatter, selfID string) error {
	if err := validateAutomationDefinition(ctx, fm, selfID, s.lookupAutomationParent); err != nil {
		return err
	}
	return validateAutomationCalendars(fm.Trigger, s.calendars)
}

// validateAutomationCalendars checks the calendar names a trigger refers to. It
// does nothing when no registry is installed. A clock trigger's calendar must
// name a builtin day calendar; a calendar trigger's calendar, and the calendar
// of skip_if_event and only_if_event, must name an ics source. The error names
// the field and the unknown or mismatched name, never a URL.
func validateAutomationCalendars(tc *frontmatter.TriggerConfig, reg *calendar.Registry) error {
	if tc == nil || reg == nil {
		return nil
	}
	// An empty trigger.calendar on a calendar trigger is rejected by
	// validateCalendarTrigger, which runs first, so only a name needs checking.
	if tc.Calendar != "" {
		want := calendar.KindBuiltin
		if tc.Type == types.TriggerTypeCalendar {
			want = calendar.KindICS
		}
		if err := checkCalendarName(reg, "trigger.calendar", tc.Calendar, want); err != nil {
			return err
		}
	}
	if err := checkEventCalendar(reg, "trigger.skip_if_event", tc.SkipIfEvent); err != nil {
		return err
	}
	return checkEventCalendar(reg, "trigger.only_if_event", tc.OnlyIfEvent)
}

// checkEventCalendar checks the calendar of a skip_if_event or only_if_event
// filter. The calendar is required and must be an ics source.
func checkEventCalendar(reg *calendar.Registry, field string, f *frontmatter.CalendarEventFilter) error {
	if f == nil {
		return nil
	}
	if f.Calendar == "" {
		return invalidAutomationField(field+".calendar", "required: name an ics calendar from server.calendars")
	}
	return checkCalendarName(reg, field+".calendar", f.Calendar, calendar.KindICS)
}

// checkCalendarName reports an error when name is not a configured source of
// the wanted kind. Unknown and wrong-kind names are rejected with the field.
func checkCalendarName(reg *calendar.Registry, field, name string, want string) error {
	kind, ok := reg.Kind(name)
	if !ok {
		return invalidAutomationField(field, fmt.Sprintf("unknown calendar %q (not in server.calendars)", name))
	}
	if kind != want {
		return invalidAutomationField(field, fmt.Sprintf("calendar %q is %s; this field needs %s",
			name, calendarKindPhrase(kind), calendarKindPhrase(want)))
	}
	return nil
}

// calendarKindPhrase describes a source kind for an error message.
func calendarKindPhrase(kind string) string {
	if kind == calendar.KindICS {
		return "an ics source"
	}
	return "a builtin day calendar"
}
