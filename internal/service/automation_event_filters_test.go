package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

// Event day filters: trigger.skip_if_event and trigger.only_if_event gate a
// clock trigger's slots by the occurrences of an ics source.
//
// The snapshots here are fake. Each test writes one in the poller's on-disk
// format and loads it through calendar.NewPoller, which is the path the server
// takes at startup, so no test reaches into the calendar package's internals.
// Tests are in November 2026 (UTC) with the 09:00 slot unless they say
// otherwise.

// fakeEvent is one occurrence of the "team" source.
type fakeEvent struct {
	title, description, location string
	start, end                   time.Time
	allDay                       bool
}

// fakeSnapshot is the state of the "team" source: its last good occurrences,
// the end of their expansion window, and its fetch bookkeeping.
type fakeSnapshot struct {
	lastSuccess time.Time
	windowEnd   time.Time
	stale       bool
	lastError   string
	events      []fakeEvent
}

// onDiskSnapshot and onDiskEvent mirror the snapshot file the poller writes
// (internal/calendar/cache.go). They are local so this test file never imports
// the calendar package's unexported types.
type onDiskSnapshot struct {
	Name        string        `json:"name"`
	LastFetch   time.Time     `json:"last_fetch"`
	LastSuccess time.Time     `json:"last_success"`
	LastError   string        `json:"last_error,omitempty"`
	Stale       bool          `json:"stale"`
	WindowEnd   time.Time     `json:"window_end"`
	Occurrences []onDiskEvent `json:"occurrences"`
}

type onDiskEvent struct {
	UID          string    `json:"uid"`
	Calendar     string    `json:"calendar"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	AllDay       bool      `json:"all_day,omitempty"`
	Title        string    `json:"title,omitempty"`
	Description  string    `json:"description,omitempty"`
	Location     string    `json:"location,omitempty"`
	RecurrenceID time.Time `json:"recurrence_id"`
}

// eventWindowEnd is where the healthy fake snapshot's expansion window ends.
// The window starts 15 days earlier, on 2026-11-20, so Nov 20 through Dec 4
// are inside it and Nov 19 and Dec 5 onward are outside.
var eventWindowEnd = slotUTC(2026, 12, 5, 0, 0, 0)

// healthySnapshot is a fresh snapshot of the "team" source holding events.
func healthySnapshot(events ...fakeEvent) *fakeSnapshot {
	return &fakeSnapshot{
		lastSuccess: slotUTC(2026, 11, 18, 0, 0, 0),
		windowEnd:   eventWindowEnd,
		events:      events,
	}
}

// eventRegistry returns the registry for the builtin XNYS calendar and the ics
// source "team", loaded the way the server loads it. A nil snapshot leaves
// "team" with no snapshot at all.
func eventRegistry(t *testing.T, snap *fakeSnapshot) *calendar.Registry {
	t.Helper()
	team := config.CalendarConfig{Type: calendar.KindICS, URLEnv: "TEAM_CAL_URL"}
	reg, err := calendar.NewRegistry(map[string]config.CalendarConfig{
		"xnys": {Type: calendar.KindBuiltin, Market: "XNYS"},
		"team": team,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	dataDir := t.TempDir()
	if snap != nil {
		writeFakeSnapshot(t, dataDir, "team", snap)
	}
	if _, err := calendar.NewPoller(reg, map[string]config.CalendarConfig{"team": team}, dataDir, calendar.PollerOptions{}); err != nil {
		t.Fatalf("NewPoller: %v", err)
	}
	return reg
}

// writeFakeSnapshot writes a snapshot file where the poller's store reads it.
func writeFakeSnapshot(t *testing.T, dataDir, name string, snap *fakeSnapshot) {
	t.Helper()
	file := onDiskSnapshot{
		Name:        name,
		LastFetch:   snap.lastSuccess,
		LastSuccess: snap.lastSuccess,
		LastError:   snap.lastError,
		Stale:       snap.stale,
		WindowEnd:   snap.windowEnd,
		Occurrences: []onDiskEvent{},
	}
	for i, e := range snap.events {
		file.Occurrences = append(file.Occurrences, onDiskEvent{
			UID:         fmt.Sprintf("%s-%d", name, i),
			Calendar:    name,
			Start:       e.start,
			End:         e.end,
			AllDay:      e.allDay,
			Title:       e.title,
			Description: e.description,
			Location:    e.location,
		})
	}
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	dir := filepath.Join(dataDir, "calendars")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
}

// novAt is the November 2026 UTC instant at hour:00 on day.
func novAt(day, hour int) time.Time {
	return slotUTC(2026, 11, day, hour, 0, 0)
}

// daysAt returns n instants, one per day, starting at first and moving by a
// whole day in UTC.
func daysAt(first time.Time, n int) []time.Time {
	out := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, first.AddDate(0, 0, i))
	}
	return out
}

// ranOnEach ticks the scheduler once per instant, in order, and returns for
// each automation the instants at which it produced a task for project.
func ranOnEach(t *testing.T, f *slotFixture, project string, ids []string, instants []time.Time) map[string][]time.Time {
	t.Helper()
	ran := make(map[string][]time.Time, len(ids))
	before := make(map[string]int, len(ids))
	for _, id := range ids {
		before[id] = tasksIn(t, f.brain, project, id)
	}
	for _, at := range instants {
		f.tick(at)
		for _, id := range ids {
			n := tasksIn(t, f.brain, project, id)
			if n > before[id] {
				ran[id] = append(ran[id], at)
			}
			before[id] = n
		}
	}
	return ran
}

// assertInstants fails unless got holds exactly the instants in want, in order.
func assertInstants(t *testing.T, got, want []time.Time) {
	t.Helper()
	if !sameInstants(got, want) {
		t.Fatalf("ran at %v, want %v", formatInstants(got), formatInstants(want))
	}
}

func formatInstants(ts []time.Time) []string {
	out := make([]string, 0, len(ts))
	for _, ts := range ts {
		out = append(out, ts.Format(time.RFC3339))
	}
	return out
}

// ---------------------------------------------------------------------------
// skip_if_event
// ---------------------------------------------------------------------------

// An out-of-office week covers Mon 23 09:00 to Fri 27 17:00. A daily 09:00
// automation skips each covered day and runs on the days either side of it.
func TestCalendarEventSkipIfEventSkipsEachCoveredDay(t *testing.T) {
	ooo := fakeEvent{title: "Out of office", start: novAt(23, 9), end: novAt(27, 17)}
	f := newSlotFixture(t, novAt(22, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, healthySnapshot(ooo)))
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Out of office"},
	}})

	got := ranOnEach(t, f, "p", []string{auto.ID}, daysAt(novAt(22, 9), 9))[auto.ID]
	assertInstants(t, got, []time.Time{novAt(22, 9), novAt(28, 9), novAt(29, 9), novAt(30, 9)})
}

// An all-day event counts on every date it covers, and its end is exclusive:
// Holiday on Thu 26 and Fri 27 skips those days, and Sat 28 runs. The timed
// "Team sync" on Tue 24 does not match an all_day filter, so Tue runs.
func TestCalendarEventSkipIfEventAllDayEventCoversEveryDateItSpans(t *testing.T) {
	holiday := fakeEvent{title: "Holiday", allDay: true, start: novAt(26, 0), end: novAt(28, 0)}
	sync := fakeEvent{title: "Team sync", start: novAt(24, 9), end: novAt(24, 10)}
	f := newSlotFixture(t, novAt(22, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, healthySnapshot(holiday, sync)))
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", AllDay: "true"},
	}})

	got := ranOnEach(t, f, "p", []string{auto.ID}, daysAt(novAt(22, 9), 9))[auto.ID]
	assertInstants(t, got, []time.Time{novAt(22, 9), novAt(23, 9), novAt(24, 9), novAt(25, 9), novAt(28, 9), novAt(29, 9), novAt(30, 9)})
}

// A title given as a regular expression ("re:") matches case-insensitively
// through the shared filter rules: "OOO:" and "Out of office" match, and
// "Office hours" does not.
func TestCalendarEventSkipIfEventRegexTitleMatch(t *testing.T) {
	events := []fakeEvent{
		{title: "OOO: Alice", start: novAt(24, 9), end: novAt(24, 10)},
		{title: "Team sync", start: novAt(25, 9), end: novAt(25, 10)},
		{title: "Out of office", start: novAt(26, 9), end: novAt(26, 10)},
		{title: "Office hours", start: novAt(27, 9), end: novAt(27, 10)},
	}
	f := newSlotFixture(t, novAt(23, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, healthySnapshot(events...)))
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "re:(?i)^(ooo|out of office)"},
	}})

	got := ranOnEach(t, f, "p", []string{auto.ID}, daysAt(novAt(23, 9), 5))[auto.ID]
	assertInstants(t, got, []time.Time{novAt(23, 9), novAt(25, 9), novAt(27, 9)})
}

// The day is the slot's local date in the trigger's timezone. A 22:00 New York
// call on Wed 25 Nov is Thu 26 03:00 UTC: it blocks Wed's 09:00 New York slot,
// and Thu's slot still runs.
func TestCalendarEventTimedEventMatchesTheSchedulesLocalDay(t *testing.T) {
	call := fakeEvent{title: "Late call", start: novAt(26, 3), end: novAt(26, 4)}
	f := newSlotFixture(t, novAt(22, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, healthySnapshot(call)))
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		Timezone:    "America/New_York",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Late call"},
	}})

	// 09:00 EST is 14:00 UTC on these days (DST ended on Nov 1).
	instants := daysAt(novAt(23, 14), 5)
	got := ranOnEach(t, f, "p", []string{auto.ID}, instants)[auto.ID]
	assertInstants(t, got, []time.Time{novAt(23, 14), novAt(24, 14), novAt(26, 14), novAt(27, 14)})
}

// A stale snapshot still gates. The poller raises the stale alert separately,
// so the last good occurrences keep their effect.
func TestCalendarEventSkipIfEventUsesAStaleSnapshot(t *testing.T) {
	ooo := fakeEvent{title: "Out of office", start: novAt(24, 9), end: novAt(24, 17)}
	snap := healthySnapshot(ooo)
	snap.stale = true
	snap.lastError = "fetch failed: timeout"
	f := newSlotFixture(t, novAt(22, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, snap))
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Out of office"},
	}})

	got := ranOnEach(t, f, "p", []string{auto.ID}, daysAt(novAt(22, 9), 5))[auto.ID]
	assertInstants(t, got, []time.Time{novAt(22, 9), novAt(23, 9), novAt(25, 9), novAt(26, 9)})
}

// ---------------------------------------------------------------------------
// only_if_event
// ---------------------------------------------------------------------------

// only_if_event allows a day only when a matching event covers it.
func TestCalendarEventOnlyIfEventRunsOnlyOnMatchingDays(t *testing.T) {
	release := fakeEvent{title: "Release day", start: novAt(24, 10), end: novAt(24, 11)}
	f := newSlotFixture(t, novAt(22, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, healthySnapshot(release)))
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		OnlyIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Release day"},
	}})

	got := ranOnEach(t, f, "p", []string{auto.ID}, daysAt(novAt(22, 9), 9))[auto.ID]
	assertInstants(t, got, []time.Time{novAt(24, 9)})
}

// When both filters are set, both apply. Location "Office" lets Mon 23 and
// Tue 24 through, but Tue has a "Sick" event, which skip_if_event blocks.
// Wed's event is at "Client site", so only_if_event blocks it.
func TestCalendarEventBothFiltersApply(t *testing.T) {
	events := []fakeEvent{
		{title: "Work from office", location: "Office", start: novAt(23, 9), end: novAt(23, 10)},
		{title: "Sick", location: "Office", start: novAt(24, 9), end: novAt(24, 10)},
		{title: "Client visit", location: "Client site", start: novAt(25, 9), end: novAt(25, 10)},
	}
	f := newSlotFixture(t, novAt(22, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, healthySnapshot(events...)))
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Sick"},
		OnlyIfEvent: &types.CalendarEventFilter{Calendar: "team", Location: "Office"},
	}})

	got := ranOnEach(t, f, "p", []string{auto.ID}, daysAt(novAt(22, 9), 5))[auto.ID]
	assertInstants(t, got, []time.Time{novAt(23, 9)})
}

// ---------------------------------------------------------------------------
// Data availability
// ---------------------------------------------------------------------------

// With no snapshot at all, skip_if_event allows every day (fail open) and
// only_if_event allows none (fail closed).
func TestCalendarEventWithoutASnapshotFailsOpenForSkipAndClosedForOnlyIf(t *testing.T) {
	f := newSlotFixture(t, novAt(22, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, nil))
	skip := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Out of office"},
	}})
	only := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		OnlyIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Release day"},
	}})

	ran := ranOnEach(t, f, "p", []string{skip.ID, only.ID}, daysAt(novAt(23, 9), 3))
	assertInstants(t, ran[skip.ID], []time.Time{novAt(23, 9), novAt(24, 9), novAt(25, 9)})
	if n := len(ran[only.ID]); n != 0 {
		t.Fatalf("only_if_event without a snapshot ran %d times, want 0", n)
	}
}

// Outside the snapshot window nothing can be confirmed, so the same fail-open
// and fail-closed rules apply. Inside the window the events still gate.
//
// Window: Nov 20 through Dec 4. The skip event (Nov 24) and the only_if
// event (Nov 23) are inside it. Ticks run Nov 19 through Dec 7.
func TestCalendarEventOutsideTheSnapshotWindowFailsOpenOrClosed(t *testing.T) {
	ooo := fakeEvent{title: "Out of office", start: novAt(24, 9), end: novAt(24, 17)}
	release := fakeEvent{title: "Release day", start: novAt(23, 10), end: novAt(23, 11)}
	f := newSlotFixture(t, novAt(18, 0), "p")
	f.svc.SetCalendars(eventRegistry(t, healthySnapshot(ooo, release)))
	skip := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Out of office"},
	}})
	only := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:    "0 9 * * *",
		OnlyIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Release day"},
	}})

	instants := daysAt(novAt(19, 9), 19) // Nov 19 through Dec 7
	ran := ranOnEach(t, f, "p", []string{skip.ID, only.ID}, instants)

	var wantSkip []time.Time
	for _, at := range instants {
		if !at.Equal(novAt(24, 9)) {
			wantSkip = append(wantSkip, at)
		}
	}
	assertInstants(t, ran[skip.ID], wantSkip)
	assertInstants(t, ran[only.ID], []time.Time{novAt(23, 9)})
}

// A binding's skip_if_event gates only the project the binding is for. The
// parent and the other project keep their own schedules.
func TestCalendarEventBindingSkipIfEventAffectsOnlyItsProject(t *testing.T) {
	ooo := fakeEvent{title: "Out of office", start: novAt(24, 9), end: novAt(24, 17)}
	f := newSlotFixture(t, novAt(22, 0), "p1", "p2")
	f.svc.SetCalendars(eventRegistry(t, healthySnapshot(ooo)))
	parent := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{
		Schedule: "0 9 * * *",
		Filter:   map[string]string{"project": "*"},
	}})
	f.saveBinding(parent.ID, "p2", bindingSpec{trigger: &types.TriggerConfig{
		Type:        types.TriggerTypeCron,
		Schedule:    "0 9 * * *",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Out of office"},
	}})

	f.tick(novAt(23, 9))
	if p1, p2 := tasksIn(t, f.brain, "p1", parent.ID), tasksIn(t, f.brain, "p2", parent.ID); p1 != 1 || p2 != 1 {
		t.Fatalf("Nov 23: p1=%d p2=%d tasks, want 1 and 1", p1, p2)
	}
	f.tick(novAt(24, 9))
	if p1, p2 := tasksIn(t, f.brain, "p1", parent.ID), tasksIn(t, f.brain, "p2", parent.ID); p1 != 2 || p2 != 1 {
		t.Fatalf("Nov 24 (out of office for p2 only): p1=%d p2=%d tasks, want 2 and 1", p1, p2)
	}
	f.tick(novAt(25, 9))
	if p1, p2 := tasksIn(t, f.brain, "p1", parent.ID), tasksIn(t, f.brain, "p2", parent.ID); p1 != 3 || p2 != 2 {
		t.Fatalf("Nov 25: p1=%d p2=%d tasks, want 3 and 2", p1, p2)
	}
}

// Day coverage edges, checked on Tue 24 Nov (UTC) through dayFiltersFor: an
// event ending exactly at midnight does not cover the day, a point event at the
// next midnight does not either, and an all-day event covers the dates from its
// start up to, not including, its end.
func TestCalendarEventDayCoverageEdges(t *testing.T) {
	cases := []struct {
		name    string
		event   fakeEvent
		allowed bool // whether Nov 24 is allowed by skip_if_event
	}{
		{"timed event ending at local midnight", fakeEvent{title: "Edge", start: novAt(23, 22), end: novAt(24, 0)}, true},
		{"timed event running past midnight", fakeEvent{title: "Edge", start: novAt(24, 23), end: slotUTC(2026, 11, 25, 0, 30, 0)}, false},
		{"timed event starting at next midnight", fakeEvent{title: "Edge", start: novAt(25, 0), end: novAt(25, 1)}, true},
		{"zero-length event inside the day", fakeEvent{title: "Edge", start: novAt(24, 12), end: novAt(24, 12)}, false},
		{"zero-length event at next midnight", fakeEvent{title: "Edge", start: novAt(25, 0), end: novAt(25, 0)}, true},
		{"all-day event ending on the day", fakeEvent{title: "Edge", allDay: true, start: novAt(23, 0), end: novAt(24, 0)}, true},
		{"all-day event on the day", fakeEvent{title: "Edge", allDay: true, start: novAt(24, 0), end: novAt(25, 0)}, false},
		{"all-day event with equal start and end", fakeEvent{title: "Edge", allDay: true, start: novAt(24, 0), end: novAt(24, 0)}, false},
		{"all-day event spanning the day", fakeEvent{title: "Edge", allDay: true, start: novAt(22, 0), end: novAt(25, 0)}, false},
	}
	brain, _, _ := newTestBrainService(t)
	svc := NewAutomationService(brain)
	day := novAt(24, 0)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc.SetCalendars(eventRegistry(t, healthySnapshot(tc.event)))
			filters := svc.dayFiltersFor(types.BrainEntry{
				ID:       "edge",
				Modified: "2026-10-08T00:00:00Z",
				Trigger: &types.TriggerConfig{
					Type:        "cron",
					Schedule:    "0 9 * * *",
					SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Edge"},
				},
			})
			if len(filters) != 1 {
				t.Fatalf("dayFiltersFor = %d filters, want 1", len(filters))
			}
			allowed, err := filters[0].Allowed(context.Background(), day)
			if err != nil || allowed != tc.allowed {
				t.Fatalf("Allowed(Nov 24) = (%v, %v), want (%v, nil)", allowed, err, tc.allowed)
			}
		})
	}
}

// The timeline projects an event-gated automation on the same days as the
// scheduler: no projection on Nov 24, the covered day.
func TestTimelineEventSkipProjectsNoCoveredDay(t *testing.T) {
	from := slotUTC(2026, 11, 22, 0, 0, 0)
	to := slotUTC(2026, 12, 1, 0, 0, 0)
	ooo := fakeEvent{title: "Out of office", start: novAt(24, 9), end: novAt(24, 17)}
	gated := types.BrainEntry{
		ID: "ooo-gated", Type: "automation", Status: "active", Title: "gated",
		Trigger: &types.TriggerConfig{
			Type: "cron", Schedule: "0 9 * * *", Timezone: "UTC",
			SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Out of office"},
			Filter:      map[string]string{"project": "*"},
		},
	}
	brain, _, _ := newTestBrainService(t)
	automation := NewAutomationService(brain)
	automation.SetCalendars(eventRegistry(t, healthySnapshot(ooo)))
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
	want := []time.Time{novAt(22, 9), novAt(23, 9), novAt(25, 9), novAt(26, 9), novAt(27, 9), novAt(28, 9), novAt(29, 9), novAt(30, 9)}
	got := projectionInstants(result.Items, gated.ID, "p")
	if !sameInstants(got, want) {
		t.Fatalf("timeline projected %v, want %v", formatInstants(got), formatInstants(want))
	}
}

// Fail-open and fail-closed warnings are logged once per automation
// modification and reason, not on every evaluated day.
func TestCalendarEventGateWarnsOncePerReasonPerModification(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx := context.Background()

	brain, _, _ := newTestBrainService(t)
	svc := NewAutomationService(brain)

	svc.SetCalendars(eventRegistry(t, nil))
	entry := types.BrainEntry{
		ID: "warn-no-snapshot", Modified: "2026-10-08T00:00:00Z",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *",
			SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "OOO"}},
	}
	filters := svc.dayFiltersFor(entry)
	if len(filters) != 1 {
		t.Fatalf("dayFiltersFor = %d filters, want 1", len(filters))
	}
	for _, day := range []time.Time{novAt(23, 0), novAt(24, 0), novAt(25, 0)} {
		if allowed, err := filters[0].Allowed(ctx, day); err != nil || !allowed {
			t.Fatalf("no snapshot, skip_if_event Allowed(%s) = (%v, %v), want (true, nil)", day, allowed, err)
		}
	}
	if n := strings.Count(buf.String(), "warn-no-snapshot"); n != 1 {
		t.Fatalf("warnings for one modification = %d, want 1; log:\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "no snapshot yet") {
		t.Fatalf("warning does not name the reason; log:\n%s", buf.String())
	}
	entry.Modified = "2026-10-09T00:00:00Z"
	filters = svc.dayFiltersFor(entry)
	if _, err := filters[0].Allowed(ctx, novAt(23, 0)); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "warn-no-snapshot"); n != 2 {
		t.Fatalf("warnings after an edit = %d, want 2", n)
	}

	svc.SetCalendars(eventRegistry(t, healthySnapshot()))
	outside := types.BrainEntry{
		ID: "warn-outside-window", Modified: "2026-10-08T00:00:00Z",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *",
			OnlyIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Release day"}},
	}
	filters = svc.dayFiltersFor(outside)
	if len(filters) != 1 {
		t.Fatalf("dayFiltersFor(outside) = %d filters, want 1", len(filters))
	}
	for _, dec := range []int{6, 7} {
		day := time.Date(2026, 12, dec, 0, 0, 0, 0, time.UTC)
		if allowed, err := filters[0].Allowed(ctx, day); err != nil || allowed {
			t.Fatalf("outside window, only_if_event Allowed(Dec %d) = (%v, %v), want (false, nil)", dec, allowed, err)
		}
	}
	if n := strings.Count(buf.String(), "warn-outside-window"); n != 1 {
		t.Fatalf("outside-window warnings = %d, want 1; log:\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "day outside the snapshot window") {
		t.Fatalf("warning does not name the window reason; log:\n%s", buf.String())
	}
}

// A binding that adds skip_if_event to an ungated parent gates only its own
// project: the timeline must apply the binding's event gate, exactly as the
// scheduler compiles each target from its effective config.
func TestTimelineBindingEventGateOnlyItsProject(t *testing.T) {
	ooo := fakeEvent{title: "Out of office", start: novAt(24, 9), end: novAt(24, 17)}
	reg := eventRegistry(t, healthySnapshot(ooo))
	brain, _, _ := newTestBrainService(t)
	brain.SetCalendars(reg)
	parent := saveGlobalDreamParent(t, brain, "Dream")
	if _, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Out-of-office dream for p1",
		Status:  "active",
		Project: "p1",
		Extends: parent,
		Trigger: &types.TriggerConfig{
			Type:        types.TriggerTypeCron,
			Schedule:    "0 9 * * *",
			Timezone:    "UTC",
			SkipIfEvent: &types.CalendarEventFilter{Calendar: "team", Title: "Out of office"},
		},
	}); err != nil {
		t.Fatalf("save event-gated binding: %v", err)
	}

	from := slotUTC(2026, 11, 22, 0, 0, 0)
	to := slotUTC(2026, 12, 1, 0, 0, 0)
	automation := NewAutomationService(brain)
	automation.SetCalendars(reg)
	automation.SetProjectLister(&stubProjectLister{projects: []string{"p1", "p2"}})
	service := NewTimelineService(brain, &timelineEventReaderFake{},
		WithTimelineClock(func() time.Time { return from }),
		WithTimelineTargets(automation))

	result, err := service.Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Time{novAt(22, 9), novAt(23, 9), novAt(25, 9), novAt(26, 9), novAt(27, 9), novAt(28, 9), novAt(29, 9), novAt(30, 9)}
	if got := projectionInstants(result.Items, parent, "p1"); !sameInstants(got, want) {
		t.Fatalf("p1 projected %v, want its binding's 09:00 runs without Nov 24: %v", formatInstants(got), formatInstants(want))
	}
	if p2 := projectionInstants(result.Items, parent, "p2"); len(p2) != 9 {
		t.Fatalf("p2 projected %d runs (%v), want the parent's 9 daily runs, ungated", len(p2), formatInstants(p2))
	}
}
