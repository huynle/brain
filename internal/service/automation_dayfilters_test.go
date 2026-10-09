package service

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

// Calendar day filters: trigger.calendar gates a clock trigger's slots by a
// builtin day calendar. Each test saves its automation first and injects the
// registry afterwards, because save-time validation rejects unknown names
// once a registry is present.

// calendarTestRegistry is a registry with the built-in XNYS calendar (plus one
// extra closure) and one ics source, which must never gate a slot.
func calendarTestRegistry(t *testing.T) *calendar.Registry {
	t.Helper()
	reg, err := calendar.NewRegistry(map[string]config.CalendarConfig{
		"xnys": {Type: "builtin", Market: "XNYS", ExtraClosed: []string{"2026-11-27"}},
		"team": {Type: "ics", URLEnv: "TEAM_CAL_URL"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return reg
}

// A 09:00 slot with calendar: xnys fires only on open days: weekends,
// Thanksgiving and the extra closure are skipped, and the next open day
// fires. The same holds through the scheduler's slot evaluation.
func TestCalendarXNYSSkipsClosedDaysInCheckScheduled(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 11, 22, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 9 * * *", Calendar: "xnys"}})
	f.svc.SetCalendars(calendarTestRegistry(t))

	// Thanksgiving week 2026: Sun 22, Mon 23, Tue 24, Wed 25 open; Thu 26
	// (Thanksgiving), Fri 27 (extra_closed), Sat 28, Sun 29 closed; Mon 30 open.
	open := map[int]bool{23: true, 24: true, 25: true, 30: true}
	want := 0
	for day := 22; day <= 30; day++ {
		f.tick(slotUTC(2026, 11, day, 9, 0, 0))
		if open[day] {
			want++
		}
		if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != want {
			t.Fatalf("after the 09:00 tick on Nov %d: %d tasks, want %d", day, n, want)
		}
	}
}

// The day is the slot's local date in the trigger's timezone, not UTC. A 21:30
// New York slot is the next UTC day; Thursday Thanksgiving (in New York) must
// close the slot even though its UTC instant falls on Friday.
func TestCalendarUsesTheSlotsLocalDate(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 11, 20, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule: "30 21 * * *", Timezone: "America/New_York", Calendar: "xnys",
	}})
	f.svc.SetCalendars(calendarTestRegistry(t))

	// Wed 25 Nov 21:30 EST is Thu 26 02:30 UTC: an open local day, fires.
	f.tick(slotUTC(2026, 11, 26, 2, 30, 30))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("Wed 21:30 New York: %d tasks, want 1", n)
	}
	// Thu 26 Nov 21:30 EST (Thanksgiving) is Fri 27 02:30 UTC: closed locally.
	f.tick(slotUTC(2026, 11, 27, 2, 30, 30))
	// Fri 27 Nov 21:30 EST is extra_closed, Sat/Sun closed: nothing new.
	f.tick(slotUTC(2026, 11, 28, 2, 30, 30))
	f.tick(slotUTC(2026, 11, 30, 2, 30, 30))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("closed local days fired: %d tasks, want 1", n)
	}
	// Mon 30 Nov 21:30 EST is Tue 1 Dec 02:30 UTC: open, fires.
	f.tick(slotUTC(2026, 12, 1, 2, 30, 30))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 2 {
		t.Fatalf("Mon 21:30 New York: %d tasks, want 2", n)
	}
}

// An unknown calendar name fails closed at runtime: no weekday slot fires.
func TestCalendarUnknownNameFailsClosed(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 11, 22, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 9 * * *", Calendar: "nope"}})
	f.svc.SetCalendars(calendarTestRegistry(t))

	for _, day := range []int{23, 24, 25} {
		f.tick(slotUTC(2026, 11, day, 9, 0, 0))
	}
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 0 {
		t.Fatalf("unknown calendar fired %d tasks, want 0", n)
	}
}

// With no registry configured, a calendar: trigger fails closed the same way.
func TestCalendarWithoutRegistryFailsClosed(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 11, 22, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 9 * * *", Calendar: "xnys"}})

	for _, day := range []int{23, 24, 25} {
		f.tick(slotUTC(2026, 11, day, 9, 0, 0))
	}
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 0 {
		t.Fatalf("no registry fired %d tasks, want 0", n)
	}
}

// An ics source is not a day calendar, so naming one as calendar: fails closed.
func TestCalendarICSSourceFailsClosedAsDayFilter(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 11, 22, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 9 * * *", Calendar: "team"}})
	f.svc.SetCalendars(calendarTestRegistry(t))

	for _, day := range []int{23, 24, 25} {
		f.tick(slotUTC(2026, 11, day, 9, 0, 0))
	}
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 0 {
		t.Fatalf("ics source fired %d tasks, want 0", n)
	}
}

// An automation without calendar: has no day filter, so it is unaffected.
func TestCalendarAbsentMeansNoDayFilter(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	svc := NewAutomationService(brain)
	svc.SetCalendars(calendarTestRegistry(t))
	entry := types.BrainEntry{ID: "no-cal", Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *"}}
	if filters := svc.dayFiltersFor(entry); len(filters) != 0 {
		t.Fatalf("dayFiltersFor without calendar = %d filters, want none", len(filters))
	}
}

// An unknown name's filter closes every day, and its warning is logged once per
// automation modification, not on every call.
func TestCalendarUnknownNameWarnsOncePerModification(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	brain, _, _ := newTestBrainService(t)
	svc := NewAutomationService(brain)
	svc.SetCalendars(calendarTestRegistry(t))

	entry := types.BrainEntry{
		ID: "warn-once-cal", Modified: "2026-10-08T00:00:00Z",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *", Calendar: "nope"},
	}
	filters := svc.dayFiltersFor(entry)
	if len(filters) != 1 {
		t.Fatalf("dayFiltersFor = %d filters, want 1", len(filters))
	}
	day := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	allowed, err := filters[0].Allowed(context.Background(), day)
	if err != nil || allowed {
		t.Fatalf("Allowed(Wed) = (%v, %v), want (false, nil)", allowed, err)
	}
	svc.dayFiltersFor(entry)
	svc.dayFiltersFor(entry)
	if n := strings.Count(buf.String(), "warn-once-cal"); n != 1 {
		t.Fatalf("warnings for one modification = %d, want 1; log:\n%s", n, buf.String())
	}

	entry.Modified = "2026-10-09T00:00:00Z"
	svc.dayFiltersFor(entry)
	if n := strings.Count(buf.String(), "warn-once-cal"); n != 2 {
		t.Fatalf("warnings after an edit = %d, want 2", n)
	}
}

// The timeline projects a calendar-gated automation on the same open days as
// the scheduler: Mon 23, Tue 24, Wed 25 and Mon 30 Nov at 09:00 UTC, and no
// projection on Thanksgiving, the extra closure, or the weekend.
func TestTimelineCalendarXNYSSkipsClosedDays(t *testing.T) {
	from := slotUTC(2026, 11, 22, 0, 0, 0)
	to := slotUTC(2026, 12, 1, 0, 0, 0)
	gated := types.BrainEntry{
		ID: "cal-gated", Type: "automation", Status: "active", Title: "gated",
		Trigger: &types.TriggerConfig{
			Type: "cron", Schedule: "0 9 * * *", Timezone: "UTC", Calendar: "xnys",
			Filter: map[string]string{"project": "*"},
		},
	}
	brain, _, _ := newTestBrainService(t)
	automation := NewAutomationService(brain)
	automation.SetCalendars(calendarTestRegistry(t))
	automation.SetProjectLister(&stubProjectLister{projects: []string{"p"}})
	lister := &projectScopedLister{byType: map[string][]types.BrainEntry{
		"automation":     {gated},
		"automation_run": nil,
		"task":           {},
		"reminder":       {},
	}}
	service := NewTimelineService(lister, &timelineEventReaderFake{},
		WithTimelineClock(func() time.Time { return from }),
		WithTimelineTargets(automation))

	result, err := service.Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Time{
		slotUTC(2026, 11, 23, 9, 0, 0),
		slotUTC(2026, 11, 24, 9, 0, 0),
		slotUTC(2026, 11, 25, 9, 0, 0),
		slotUTC(2026, 11, 30, 9, 0, 0),
	}
	got := projectionInstants(result.Items, gated.ID, "p")
	if !sameInstants(got, want) {
		t.Fatalf("timeline projected %v, want open days %v", got, want)
	}
}

// A timeline with no resolver still fails closed for calendar: automations.
func TestTimelineCalendarWithoutServiceProjectsNothing(t *testing.T) {
	from := slotUTC(2026, 11, 22, 0, 0, 0)
	to := slotUTC(2026, 11, 30, 0, 0, 0)
	gated := types.BrainEntry{
		ID: "cal-no-service", Type: "automation", Status: "active", Title: "gated", ProjectID: "p",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *", Timezone: "UTC", Calendar: "xnys"},
	}
	lister := &projectScopedLister{byType: map[string][]types.BrainEntry{
		"automation": {gated}, "automation_run": nil, "task": {}, "reminder": {},
	}}
	service := NewTimelineService(lister, &timelineEventReaderFake{}, WithTimelineClock(func() time.Time { return from }))

	result, err := service.Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := projectionInstants(result.Items, gated.ID, "p"); len(got) != 0 {
		t.Fatalf("timeline without a calendar registry projected %v, want none", got)
	}
}
