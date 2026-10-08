package calendar

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%s): %v", name, err)
	}
	return loc
}

// line renders the identity of an occurrence compactly for slice diffs.
func line(o Occurrence) string {
	rid := "-"
	if !o.RecurrenceID.IsZero() {
		rid = o.RecurrenceID.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("%s %s %s %q allday=%t rid=%s",
		o.Start.UTC().Format(time.RFC3339), o.End.UTC().Format(time.RFC3339),
		o.UID, o.Title, o.AllDay, rid)
}

func lines(occs []Occurrence) []string {
	out := make([]string, len(occs))
	for i, o := range occs {
		out[i] = line(o)
	}
	return out
}

func assertLines(t *testing.T, got []Occurrence, want ...string) {
	t.Helper()
	g := lines(got)
	if !slices.Equal(g, want) {
		t.Fatalf("occurrences mismatch\n got:\n  %s\nwant:\n  %s",
			strings.Join(g, "\n  "), strings.Join(want, "\n  "))
	}
}

func parseFixture(t *testing.T, name string) *Feed {
	t.Helper()
	f, err := Parse(bytes.NewReader(loadFixture(t, name)))
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return f
}

func expand(t *testing.T, f *Feed, start, end time.Time, opts ExpandOptions) []Occurrence {
	t.Helper()
	occs, err := f.ExpandWithOptions(start, end, opts)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	return occs
}

func TestExpand_GoogleWeeklyRRuleExdateAndMovedInstance(t *testing.T) {
	f := parseFixture(t, "google_weekly.ics")

	occs, err := f.Expand(utc("2025-10-01T00:00:00Z"), utc("2025-11-11T00:00:00Z"))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	// Weekly Monday 10:00 America/New_York: Oct 20 is EXDATEd, Oct 27 is
	// moved to Tue Oct 28 14:00, and after DST ends (Nov 2) the wall-clock
	// time stays 10:00, so the UTC instant shifts from 14:00 to 15:00.
	assertLines(t, occs,
		`2025-10-06T14:00:00Z 2025-10-06T14:30:00Z standup-7k2j@google.com "Team Standup" allday=false rid=2025-10-06T14:00:00Z`,
		`2025-10-13T14:00:00Z 2025-10-13T14:30:00Z standup-7k2j@google.com "Team Standup" allday=false rid=2025-10-13T14:00:00Z`,
		`2025-10-15T17:00:00Z 2025-10-15T18:00:00Z lunch-q81x@google.com "Lunch with Sam" allday=false rid=-`,
		`2025-10-28T18:00:00Z 2025-10-28T18:30:00Z standup-7k2j@google.com "Team Standup (moved)" allday=false rid=2025-10-27T14:00:00Z`,
		`2025-11-03T15:00:00Z 2025-11-03T15:30:00Z standup-7k2j@google.com "Team Standup" allday=false rid=2025-11-03T15:00:00Z`,
		`2025-11-10T15:00:00Z 2025-11-10T15:30:00Z standup-7k2j@google.com "Team Standup" allday=false rid=2025-11-10T15:00:00Z`,
	)

	first := occs[0]
	if want := "Weekly sync, bring notes.\nAgenda: blockers; demos. This line is folded by the exporter."; first.Description != want {
		t.Errorf("Description = %q, want %q", first.Description, want)
	}
	if first.Location != "Room 4, HQ" {
		t.Errorf("Location = %q", first.Location)
	}
	if first.Calendar != "" {
		t.Errorf("Calendar = %q, want empty (attached by the poller)", first.Calendar)
	}
	moved := occs[3]
	if moved.Location != "Room 9" || moved.Description != "Moved to Tuesday this week." {
		t.Errorf("moved instance kept master fields: %+v", moved)
	}

	t.Run("original slot of the moved instance is empty", func(t *testing.T) {
		ny := mustLoad(t, "America/New_York")
		occs := expand(t, f, time.Date(2025, 10, 27, 0, 0, 0, 0, ny), time.Date(2025, 10, 28, 0, 0, 0, 0, ny), ExpandOptions{})
		assertLines(t, occs)
	})
	t.Run("moved instance appears in a window excluding its original slot", func(t *testing.T) {
		occs := expand(t, f, utc("2025-10-28T00:00:00Z"), utc("2025-10-29T00:00:00Z"), ExpandOptions{})
		assertLines(t, occs,
			`2025-10-28T18:00:00Z 2025-10-28T18:30:00Z standup-7k2j@google.com "Team Standup (moved)" allday=false rid=2025-10-27T14:00:00Z`)
	})
}

func TestExpand_AllDayEventsAreDateSpans(t *testing.T) {
	f := parseFixture(t, "allday.ics")
	ny := mustLoad(t, "America/New_York")
	opts := ExpandOptions{DefaultLocation: ny}

	occs := expand(t, f, time.Date(2025, 10, 1, 0, 0, 0, 0, ny), time.Date(2025, 11, 11, 0, 0, 0, 0, ny), opts)
	// Midnight-to-midnight in the default location; DTEND exclusive; the
	// trip crosses the Nov 2 DST change so its UTC end is 05:00, not 04:00.
	assertLines(t, occs,
		`2025-10-20T04:00:00Z 2025-10-23T04:00:00Z kubecon-2025@google.com "KubeCon" allday=true rid=-`,
		`2025-10-25T04:00:00Z 2025-10-26T04:00:00Z bday-alex@google.com "Alex's birthday" allday=true rid=2025-10-25T04:00:00Z`,
		`2025-10-27T04:00:00Z 2025-10-28T04:00:00Z office-closed@google.com "Office closed" allday=true rid=-`,
		`2025-11-01T04:00:00Z 2025-11-03T05:00:00Z trip-dst@google.com "Weekend trip" allday=true rid=-`,
	)
	for _, o := range occs {
		for _, ts := range []time.Time{o.Start, o.End} {
			if ts.Location() != ny || ts.Hour() != 0 || ts.Minute() != 0 {
				t.Errorf("%s: %v is not local midnight in %v", o.UID, ts, ny)
			}
		}
	}

	t.Run("multi-day event overlaps a window inside its span", func(t *testing.T) {
		occs := expand(t, f, time.Date(2025, 10, 21, 12, 0, 0, 0, ny), time.Date(2025, 10, 21, 13, 0, 0, 0, ny), opts)
		assertLines(t, occs,
			`2025-10-20T04:00:00Z 2025-10-23T04:00:00Z kubecon-2025@google.com "KubeCon" allday=true rid=-`)
	})
	t.Run("DTEND date is exclusive", func(t *testing.T) {
		occs := expand(t, f, time.Date(2025, 10, 23, 0, 0, 0, 0, ny), time.Date(2025, 10, 24, 0, 0, 0, 0, ny), opts)
		assertLines(t, occs)
	})
	t.Run("default location is UTC", func(t *testing.T) {
		occs := expand(t, f, utc("2025-10-20T00:00:00Z"), utc("2025-10-21T00:00:00Z"), ExpandOptions{})
		assertLines(t, occs,
			`2025-10-20T00:00:00Z 2025-10-23T00:00:00Z kubecon-2025@google.com "KubeCon" allday=true rid=-`)
	})
}

func TestExpand_CancelledEventsAndInstancesAreDropped(t *testing.T) {
	f := parseFixture(t, "cancelled.ics")
	occs := expand(t, f, utc("2025-10-13T00:00:00Z"), utc("2025-10-25T00:00:00Z"), ExpandOptions{})
	// Daily x5 from Oct 13 with Oct 15 cancelled via a RECURRENCE-ID
	// override; the standalone cancelled event and the cancelled series
	// (including its edited, non-cancelled instance) produce nothing.
	assertLines(t, occs,
		`2025-10-13T13:00:00Z 2025-10-13T13:15:00Z daily-checkin@google.com "Daily check-in" allday=false rid=2025-10-13T13:00:00Z`,
		`2025-10-14T13:00:00Z 2025-10-14T13:15:00Z daily-checkin@google.com "Daily check-in" allday=false rid=2025-10-14T13:00:00Z`,
		`2025-10-16T13:00:00Z 2025-10-16T13:15:00Z daily-checkin@google.com "Daily check-in" allday=false rid=2025-10-16T13:00:00Z`,
		`2025-10-17T13:00:00Z 2025-10-17T13:15:00Z daily-checkin@google.com "Daily check-in" allday=false rid=2025-10-17T13:00:00Z`,
	)
}

const outlookUID = "040000008200E00074C5B7101A82E00800000000A0B1C2D3E4F5A6B7C8D9E0F1A2B3C4D5"

func TestExpand_OutlookWindowsTimeZones(t *testing.T) {
	f := parseFixture(t, "outlook_windows_tz.ics")
	occs := expand(t, f, utc("2025-10-13T00:00:00Z"), utc("2025-11-12T00:00:00Z"), ExpandOptions{})
	id := func(n int) string {
		return fmt.Sprintf("040000008200E00074C5B7101A82E0080000000000000000000000000000000000000000%02d", n)
	}
	assertLines(t, occs,
		`2025-10-15T13:00:00Z 2025-10-15T14:00:00Z `+outlookUID+` "Architecture review" allday=false rid=2025-10-15T13:00:00Z`,
		`2025-10-16T07:00:00Z 2025-10-16T07:30:00Z `+id(3)+` "Berlin sync" allday=false rid=-`,
		`2025-10-16T08:00:00Z 2025-10-16T08:30:00Z `+id(2)+` "London sync" allday=false rid=-`,
		`2025-10-16T16:00:00Z 2025-10-16T16:30:00Z `+id(1)+` "Vendor call (Seattle)" allday=false rid=-`,
		`2025-10-17T00:00:00Z 2025-10-18T00:00:00Z `+id(4)+` "Offsite" allday=true rid=-`,
		`2025-10-29T13:00:00Z 2025-10-29T14:00:00Z `+outlookUID+` "Architecture review" allday=false rid=2025-10-29T13:00:00Z`,
		`2025-11-05T14:00:00Z 2025-11-05T15:00:00Z `+outlookUID+` "Architecture review" allday=false rid=2025-11-05T14:00:00Z`,
	)
}

func TestExpand_TZIDResolutionFallbacks(t *testing.T) {
	f := mustParse(t, `
BEGIN:VTIMEZONE
TZID:Custom Zone 1
X-LIC-LOCATION:Europe/Paris
END:VTIMEZONE
BEGIN:VEVENT
UID:a-mozilla-path
DTSTART;TZID=/mozilla.org/20050126_1/America/New_York:20251015T090000
END:VEVENT
BEGIN:VEVENT
UID:b-lic-location
DTSTART;TZID="Custom Zone 1":20251015T090000
END:VEVENT
BEGIN:VEVENT
UID:c-windows-lowercase
DTSTART;TZID=eastern standard time:20251015T090000
END:VEVENT
BEGIN:VEVENT
UID:d-unknown-is-floating
DTSTART;TZID=Mars/Olympus_Mons:20251015T090000
END:VEVENT`)
	if len(f.Warnings) != 1 || !strings.Contains(f.Warnings[0], "d-unknown-is-floating") {
		t.Fatalf("want exactly one warning for the unknown zone, got %q", f.Warnings)
	}

	tokyo := mustLoad(t, "Asia/Tokyo")
	occs := expand(t, f, utc("2025-10-14T00:00:00Z"), utc("2025-10-16T00:00:00Z"), ExpandOptions{DefaultLocation: tokyo})
	// Equal starts sort by UID (a before c).
	assertLines(t, occs,
		`2025-10-15T00:00:00Z 2025-10-15T00:00:00Z d-unknown-is-floating "" allday=false rid=-`,
		`2025-10-15T07:00:00Z 2025-10-15T07:00:00Z b-lic-location "" allday=false rid=-`,
		`2025-10-15T13:00:00Z 2025-10-15T13:00:00Z a-mozilla-path "" allday=false rid=-`,
		`2025-10-15T13:00:00Z 2025-10-15T13:00:00Z c-windows-lowercase "" allday=false rid=-`,
	)
}

func TestWindowsZones_RequiredNamesAndAllTargetsLoad(t *testing.T) {
	r := &tzResolver{licLocation: map[string]string{}, cache: map[string]*time.Location{}}
	for win, want := range map[string]string{
		"Eastern Standard Time":   "America/New_York",
		"Pacific Standard Time":   "America/Los_Angeles",
		"GMT Standard Time":       "Europe/London",
		"W. Europe Standard Time": "Europe/Berlin",
		"Central Standard Time":   "America/Chicago",
		"Mountain Standard Time":  "America/Denver",
	} {
		if loc := r.resolve(win); loc == nil || loc.String() != want {
			t.Errorf("resolve(%q) = %v, want %s", win, loc, want)
		}
	}
	for win, iana := range windowsZones {
		if win != strings.ToLower(win) {
			t.Errorf("windowsZones key %q must be lower-case", win)
		}
		if _, err := time.LoadLocation(iana); err != nil {
			t.Errorf("windowsZones[%q] = %q does not load: %v", win, iana, err)
		}
	}
	if r.resolve("Local") != nil {
		t.Error(`TZID "Local" must not resolve to the server's zone`)
	}
}

func TestExpand_FloatingTimesUseDefaultLocation(t *testing.T) {
	f := mustParse(t, `
BEGIN:VEVENT
UID:floating
DTSTART:20251015T090000
DTEND:20251015T100000
RRULE:FREQ=DAILY;COUNT=3
EXDATE:20251016T090000
END:VEVENT`)
	window := func() (time.Time, time.Time) { return utc("2025-10-14T00:00:00Z"), utc("2025-10-20T00:00:00Z") }

	s, e := window()
	assertLines(t, expand(t, f, s, e, ExpandOptions{}),
		`2025-10-15T09:00:00Z 2025-10-15T10:00:00Z floating "" allday=false rid=2025-10-15T09:00:00Z`,
		`2025-10-17T09:00:00Z 2025-10-17T10:00:00Z floating "" allday=false rid=2025-10-17T09:00:00Z`,
	)
	assertLines(t, expand(t, f, s, e, ExpandOptions{DefaultLocation: mustLoad(t, "America/New_York")}),
		`2025-10-15T13:00:00Z 2025-10-15T14:00:00Z floating "" allday=false rid=2025-10-15T13:00:00Z`,
		`2025-10-17T13:00:00Z 2025-10-17T14:00:00Z floating "" allday=false rid=2025-10-17T13:00:00Z`,
	)
}

func TestExpand_RDateAndExdateLists(t *testing.T) {
	f := mustParse(t, `
BEGIN:VEVENT
UID:lists
DTSTART:20251001T120000Z
DTEND:20251001T130000Z
RRULE:FREQ=DAILY;COUNT=5
EXDATE:20251002T120000Z,20251004T120000Z
RDATE:20251010T120000Z,20251001T120000Z
RDATE;VALUE=PERIOD:20251012T120000Z/PT1H
END:VEVENT`)
	if len(f.Warnings) != 0 {
		t.Fatalf("unexpected warnings %q", f.Warnings)
	}
	assertLines(t, expand(t, f, utc("2025-10-01T00:00:00Z"), utc("2025-11-01T00:00:00Z"), ExpandOptions{}),
		`2025-10-01T12:00:00Z 2025-10-01T13:00:00Z lists "" allday=false rid=2025-10-01T12:00:00Z`,
		`2025-10-03T12:00:00Z 2025-10-03T13:00:00Z lists "" allday=false rid=2025-10-03T12:00:00Z`,
		`2025-10-05T12:00:00Z 2025-10-05T13:00:00Z lists "" allday=false rid=2025-10-05T12:00:00Z`,
		`2025-10-10T12:00:00Z 2025-10-10T13:00:00Z lists "" allday=false rid=2025-10-10T12:00:00Z`,
		`2025-10-12T12:00:00Z 2025-10-12T13:00:00Z lists "" allday=false rid=2025-10-12T12:00:00Z`,
	)
}

func TestExpand_EndsFromDurationAndDefaults(t *testing.T) {
	f := mustParse(t, `
BEGIN:VEVENT
UID:a-duration
DTSTART:20251015T090000Z
DURATION:PT90M
END:VEVENT
BEGIN:VEVENT
UID:b-allday-duration
DTSTART;VALUE=DATE:20251015
DURATION:P2D
END:VEVENT
BEGIN:VEVENT
UID:c-instant
DTSTART:20251015T120000Z
END:VEVENT
BEGIN:VEVENT
UID:d-instant-at-window-end
DTSTART:20251016T000000Z
END:VEVENT`)
	assertLines(t, expand(t, f, utc("2025-10-15T00:00:00Z"), utc("2025-10-16T00:00:00Z"), ExpandOptions{}),
		`2025-10-15T00:00:00Z 2025-10-17T00:00:00Z b-allday-duration "" allday=true rid=-`,
		`2025-10-15T09:00:00Z 2025-10-15T10:30:00Z a-duration "" allday=false rid=-`,
		`2025-10-15T12:00:00Z 2025-10-15T12:00:00Z c-instant "" allday=false rid=-`,
	)
}

func TestExpand_OverrideEdgeCases(t *testing.T) {
	f := mustParse(t, `
BEGIN:VEVENT
UID:series
DTSTART:20251013T150000Z
DTEND:20251013T160000Z
RRULE:FREQ=DAILY;COUNT=3
END:VEVENT
BEGIN:VEVENT
UID:series
RECURRENCE-ID:20251014T150000Z
DTSTART:20251014T170000Z
DTEND:20251014T180000Z
SEQUENCE:1
SUMMARY:older edit
END:VEVENT
BEGIN:VEVENT
UID:series
RECURRENCE-ID:20251014T150000Z
DTSTART:20251014T190000Z
DTEND:20251014T200000Z
SEQUENCE:2
SUMMARY:latest edit
END:VEVENT
BEGIN:VEVENT
UID:orphan
RECURRENCE-ID:20251013T080000Z
DTSTART:20251013T090000Z
DTEND:20251013T093000Z
SUMMARY:instance shared without its series
END:VEVENT
BEGIN:VEVENT
DTSTART:20251015T080000Z
SUMMARY:no uid one
END:VEVENT
BEGIN:VEVENT
DTSTART:20251015T080000Z
SUMMARY:no uid two
END:VEVENT`)
	assertLines(t, expand(t, f, utc("2025-10-13T00:00:00Z"), utc("2025-10-20T00:00:00Z"), ExpandOptions{}),
		`2025-10-13T09:00:00Z 2025-10-13T09:30:00Z orphan "instance shared without its series" allday=false rid=2025-10-13T08:00:00Z`,
		`2025-10-13T15:00:00Z 2025-10-13T16:00:00Z series "" allday=false rid=2025-10-13T15:00:00Z`,
		`2025-10-14T19:00:00Z 2025-10-14T20:00:00Z series "latest edit" allday=false rid=2025-10-14T15:00:00Z`,
		`2025-10-15T08:00:00Z 2025-10-15T08:00:00Z  "no uid one" allday=false rid=-`,
		`2025-10-15T08:00:00Z 2025-10-15T08:00:00Z  "no uid two" allday=false rid=-`,
		`2025-10-15T15:00:00Z 2025-10-15T16:00:00Z series "" allday=false rid=2025-10-15T15:00:00Z`,
	)
}

func TestExpand_PerEventOccurrenceCap(t *testing.T) {
	f := mustParse(t, `
BEGIN:VEVENT
UID:every-minute
DTSTART:20251001T000000Z
RRULE:FREQ=MINUTELY
END:VEVENT`)
	start, end := utc("2025-10-01T00:00:00Z"), utc("2025-10-02T00:00:00Z")

	if got := len(expand(t, f, start, end, ExpandOptions{})); got != DefaultMaxOccurrencesPerEvent {
		t.Fatalf("default cap: got %d occurrences, want %d", got, DefaultMaxOccurrencesPerEvent)
	}
	assertLines(t, expand(t, f, start, end, ExpandOptions{MaxOccurrencesPerEvent: 2}),
		`2025-10-01T00:00:00Z 2025-10-01T00:00:00Z every-minute "" allday=false rid=2025-10-01T00:00:00Z`,
		`2025-10-01T00:01:00Z 2025-10-01T00:01:00Z every-minute "" allday=false rid=2025-10-01T00:01:00Z`,
	)
}

// withTimeout fails the test instead of hanging when fn does not return.
func withTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not finish within %v (runaway recurrence rule)", d)
	}
}

func TestExpand_UnsafeRulesAreIgnoredNotHung(t *testing.T) {
	// rrule-go loops forever inside a single Next() call when a sub-daily
	// interval can never reach a BYHOUR/BYMINUTE/BYSECOND value.
	body := `
BEGIN:VEVENT
UID:hourly-unreachable
DTSTART:20251015T000000Z
RRULE:FREQ=HOURLY;INTERVAL=24;BYHOUR=5
END:VEVENT
BEGIN:VEVENT
UID:minutely-unreachable
DTSTART:20251015T010000Z
RRULE:FREQ=MINUTELY;INTERVAL=60;BYMINUTE=30
END:VEVENT
BEGIN:VEVENT
UID:secondly-unreachable
DTSTART:20251015T020000Z
RRULE:FREQ=SECONDLY;INTERVAL=60;BYSECOND=30
END:VEVENT
BEGIN:VEVENT
UID:huge-interval
DTSTART:20251015T030000Z
RRULE:FREQ=HOURLY;INTERVAL=9223372036854775807
END:VEVENT
BEGIN:VEVENT
UID:oversized-rule
DTSTART:20251015T040000Z
RRULE:FREQ=DAILY;BYMONTHDAY=` + strings.Repeat("1,", 400) + `1
END:VEVENT
BEGIN:VEVENT
UID:hourly-reachable
DTSTART:20251015T000000Z
RRULE:FREQ=HOURLY;INTERVAL=5;BYHOUR=3;COUNT=1
END:VEVENT`
	var f *Feed
	var occs []Occurrence
	withTimeout(t, 10*time.Second, func() {
		var err error
		if f, err = Parse(strings.NewReader(wrapCalendar(body))); err != nil {
			t.Errorf("Parse: %v", err)
			return
		}
		occs, err = f.Expand(utc("2025-10-14T00:00:00Z"), utc("2025-10-19T00:00:00Z"))
		if err != nil {
			t.Errorf("Expand: %v", err)
		}
	})
	if t.Failed() {
		return
	}
	wantWarn := []string{"hourly-unreachable", "minutely-unreachable", "secondly-unreachable", "huge-interval", "oversized-rule"}
	if len(f.Warnings) != len(wantWarn) {
		t.Fatalf("got warnings %q, want one each for %v", f.Warnings, wantWarn)
	}
	for i, uid := range wantWarn {
		if !strings.Contains(f.Warnings[i], uid) {
			t.Errorf("warning %d = %q, want %q", i, f.Warnings[i], uid)
		}
	}
	// Ignored rules degrade to the DTSTART instance. The reachable rule
	// keeps DTSTART as its first instance (RFC 5545 3.3.10) and its hours
	// 05, 10, 15, 20, 01, 06, ... first reach 03 on Oct 18.
	assertLines(t, occs,
		`2025-10-15T00:00:00Z 2025-10-15T00:00:00Z hourly-reachable "" allday=false rid=2025-10-15T00:00:00Z`,
		`2025-10-15T00:00:00Z 2025-10-15T00:00:00Z hourly-unreachable "" allday=false rid=-`,
		`2025-10-15T01:00:00Z 2025-10-15T01:00:00Z minutely-unreachable "" allday=false rid=-`,
		`2025-10-15T02:00:00Z 2025-10-15T02:00:00Z secondly-unreachable "" allday=false rid=-`,
		`2025-10-15T03:00:00Z 2025-10-15T03:00:00Z huge-interval "" allday=false rid=-`,
		`2025-10-15T04:00:00Z 2025-10-15T04:00:00Z oversized-rule "" allday=false rid=-`,
		`2025-10-18T03:00:00Z 2025-10-18T03:00:00Z hourly-reachable "" allday=false rid=2025-10-18T03:00:00Z`,
	)
}

func TestExpand_ScanCapStopsRulesThatNeverReachTheWindow(t *testing.T) {
	// ~13M minutely candidates precede the window; the per-event scan cap
	// stops the walk long before that.
	f := mustParse(t, `
BEGIN:VEVENT
UID:old-minutely
DTSTART:20000101T000000Z
RRULE:FREQ=MINUTELY
END:VEVENT`)
	withTimeout(t, 10*time.Second, func() {
		occs, err := f.Expand(utc("2025-10-01T00:00:00Z"), utc("2025-10-02T00:00:00Z"))
		if err != nil {
			t.Errorf("Expand: %v", err)
		}
		if len(occs) != 0 {
			t.Errorf("got %d occurrences, want 0 (scan cap reached before window)", len(occs))
		}
	})
}

func TestExpand_FeedLevelLimits(t *testing.T) {
	t.Run("total occurrences", func(t *testing.T) {
		var b strings.Builder
		for i := range defaultLimits.occurrencesPerExpand/DefaultMaxOccurrencesPerEvent + 1 {
			fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:m%d\r\nDTSTART:20251001T000000Z\r\nRRULE:FREQ=MINUTELY\r\nEND:VEVENT\r\n", i)
		}
		f := mustParse(t, b.String())
		_, err := f.Expand(utc("2025-10-01T00:00:00Z"), utc("2025-10-02T00:00:00Z"))
		if !errors.Is(err, ErrExpansionLimit) {
			t.Fatalf("want ErrExpansionLimit, got %v", err)
		}
	})
	t.Run("total scan budget", func(t *testing.T) {
		// Small limits keep this fast; the defaults would walk 2M instances.
		lim := limits{scanPerEvent: 1_000, scanPerExpand: 20_000, occurrencesPerExpand: 100}
		var b strings.Builder
		for i := range lim.scanPerExpand / lim.scanPerEvent {
			fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:s%d\r\nDTSTART:20000101T000000Z\r\nRRULE:FREQ=MINUTELY\r\nEND:VEVENT\r\n", i)
		}
		start, end := utc("2025-10-01T00:00:00Z"), utc("2025-10-02T00:00:00Z")
		f := mustParse(t, b.String())
		if occs, err := f.expand(start, end, ExpandOptions{}, lim); err != nil || len(occs) != 0 {
			t.Fatalf("at the budget: got %d occurrences, err %v; want none, no error", len(occs), err)
		}
		f = mustParse(t, b.String()+"BEGIN:VEVENT\r\nUID:one-more\r\nDTSTART:20000101T000000Z\r\nRRULE:FREQ=MINUTELY\r\nEND:VEVENT")
		if _, err := f.expand(start, end, ExpandOptions{}, lim); !errors.Is(err, ErrExpansionLimit) {
			t.Fatalf("over the budget: want ErrExpansionLimit, got %v", err)
		}
	})
}

func TestExpand_WindowValidation(t *testing.T) {
	f := parseFixture(t, "google_weekly.ics")
	if _, err := f.Expand(utc("2025-10-02T00:00:00Z"), utc("2025-10-01T00:00:00Z")); err == nil {
		t.Fatal("end before start must be an error")
	}
	occs, err := f.Expand(utc("2025-10-06T14:00:00Z"), utc("2025-10-06T14:00:00Z"))
	if err != nil || len(occs) != 0 {
		t.Fatalf("empty window: got %d occurrences, err %v", len(occs), err)
	}
	var nilFeed *Feed
	if _, err := nilFeed.Expand(utc("2025-10-01T00:00:00Z"), utc("2025-10-02T00:00:00Z")); err == nil {
		t.Fatal("nil feed must be an error")
	}
}

func TestExpand_DeterministicAndConcurrencySafe(t *testing.T) {
	f := parseFixture(t, "outlook_windows_tz.ics")
	start, end := utc("2025-10-01T00:00:00Z"), utc("2025-12-01T00:00:00Z")
	want := lines(expand(t, f, start, end, ExpandOptions{}))

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			occs, err := f.Expand(start, end)
			if err != nil {
				t.Errorf("Expand: %v", err)
				return
			}
			if got := lines(occs); !slices.Equal(got, want) {
				t.Errorf("non-deterministic expansion:\n%q\nvs\n%q", got, want)
			}
		}()
	}
	wg.Wait()
}
