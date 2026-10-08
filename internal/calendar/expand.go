package calendar

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// DefaultMaxOccurrencesPerEvent caps how many instances one recurring event
// contributes to a single expansion when ExpandOptions does not override it.
const DefaultMaxOccurrencesPerEvent = 1000

// limits bounds the work of one expansion.
type limits struct {
	// scanPerEvent bounds the recurrence instances walked for one event,
	// including those before the window (rrule-go always starts at
	// DTSTART). An event that has not reached the window by then is
	// truncated.
	scanPerEvent int
	// scanPerExpand and occurrencesPerExpand bound one whole expansion;
	// exceeding either returns ErrExpansionLimit.
	scanPerExpand        int
	occurrencesPerExpand int
}

// defaultLimits: 100k instances covers a daily event since 1750 or an
// hourly one for ~11 years; the feed-wide budgets keep a hostile feed to
// well under a second of rrule work and a bounded result.
var defaultLimits = limits{
	scanPerEvent:         100_000,
	scanPerExpand:        2_000_000,
	occurrencesPerExpand: 100_000,
}

// ErrExpansionLimit is returned when expanding a feed would exceed the
// feed-wide work or result limits.
var ErrExpansionLimit = errors.New("calendar: expansion limit exceeded")

// Occurrence is one concrete instance of a calendar event.
type Occurrence struct {
	UID string
	// Calendar is the configured source name. Expand leaves it empty; the
	// caller that owns the source attaches it.
	Calendar string
	// Start is inclusive and End exclusive. For AllDay occurrences both are
	// local midnight in ExpandOptions.DefaultLocation, so compare their
	// calendar dates rather than converting them to another zone.
	Start, End  time.Time
	AllDay      bool
	Title       string
	Description string
	Location    string
	// RecurrenceID identifies the instance within a recurring series: the
	// instance's original (unmoved) start. Zero for non-recurring events.
	RecurrenceID time.Time
}

// ExpandOptions tunes expansion. The zero value is valid.
type ExpandOptions struct {
	// DefaultLocation places floating times (no TZID and no "Z"), all-day
	// dates, and values with unresolvable TZIDs. Nil means UTC.
	DefaultLocation *time.Location
	// MaxOccurrencesPerEvent caps instances per recurring event; further
	// instances are silently dropped. Zero or negative means
	// DefaultMaxOccurrencesPerEvent.
	MaxOccurrencesPerEvent int
}

// Expand is ExpandWithOptions with zero options (floating times in UTC).
func (f *Feed) Expand(start, end time.Time) ([]Occurrence, error) {
	return f.ExpandWithOptions(start, end, ExpandOptions{})
}

// ExpandWithOptions returns every occurrence overlapping [start, end),
// sorted by Start, then UID (then RecurrenceID, End, Title; remaining ties
// keep feed order). An occurrence overlaps when it starts before end and
// ends after start; a zero-length occurrence must start within the window.
//
// Recurring events expand RRULE (DTSTART is always the first instance),
// RDATE, and EXDATE. A RECURRENCE-ID override replaces the instance it
// names, so a moved instance appears at its new time only. Cancelled events
// and cancelled instances are omitted; a cancelled master drops its whole
// series, overrides included. A panic inside recurrence iteration (an
// rrule-go bug that parse-time validation missed) is returned as an error
// naming the event instead of propagating.
func (f *Feed) ExpandWithOptions(start, end time.Time, opts ExpandOptions) ([]Occurrence, error) {
	return f.expand(start, end, opts, defaultLimits)
}

func (f *Feed) expand(start, end time.Time, opts ExpandOptions, lim limits) ([]Occurrence, error) {
	if f == nil {
		return nil, errors.New("calendar: expand nil feed")
	}
	if end.Before(start) {
		return nil, fmt.Errorf("calendar: window end %s before start %s",
			end.Format(time.RFC3339), start.Format(time.RFC3339))
	}
	x := &expander{loc: opts.DefaultLocation, start: start, end: end, perEvent: opts.MaxOccurrencesPerEvent, lim: lim}
	if x.loc == nil {
		x.loc = time.UTC
	}
	if x.perEvent <= 0 {
		x.perEvent = DefaultMaxOccurrencesPerEvent
	}
	for _, g := range f.groups {
		if err := x.expandGroup(g); err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(x.out, compareOccurrences)
	return x.out, nil
}

func compareOccurrences(a, b Occurrence) int {
	if c := a.Start.Compare(b.Start); c != 0 {
		return c
	}
	if c := strings.Compare(a.UID, b.UID); c != 0 {
		return c
	}
	if c := a.RecurrenceID.Compare(b.RecurrenceID); c != 0 {
		return c
	}
	if c := a.End.Compare(b.End); c != 0 {
		return c
	}
	return strings.Compare(a.Title, b.Title)
}

type expander struct {
	loc        *time.Location
	start, end time.Time
	perEvent   int
	lim        limits
	scanned    int
	out        []Occurrence
}

func (x *expander) expandGroup(g *eventGroup) error {
	if g.master != nil && g.master.cancelled {
		return nil
	}
	// Latest override per instance: highest SEQUENCE, then later in feed.
	byKey := map[int64]*vevent{}
	for _, ov := range g.overrides {
		k := ov.recurrenceID.in(x.loc).Unix()
		if cur, ok := byKey[k]; !ok || ov.sequence >= cur.sequence {
			byKey[k] = ov
		}
	}
	if g.master != nil {
		if err := x.expandMaster(g.master, byKey); err != nil {
			return err
		}
	}
	for _, ov := range g.overrides {
		if byKey[ov.recurrenceID.in(x.loc).Unix()] != ov || ov.cancelled {
			continue
		}
		s := ov.start.in(x.loc)
		if e := x.spanOf(ov, s)(s); x.overlaps(s, e) {
			if err := x.emit(ov, s, e, ov.recurrenceID.in(x.loc)); err != nil {
				return err
			}
		}
	}
	return nil
}

// expandMaster emits the master's instances that overlap the window,
// skipping EXDATEs and instances replaced by overrides.
func (x *expander) expandMaster(ev *vevent, overridden map[int64]*vevent) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("calendar: event %q: recurrence expansion panicked: %v", truncate(ev.uid, 80), rec)
		}
	}()

	s0 := ev.start.in(x.loc)
	span := x.spanOf(ev, s0)
	if ev.rrule == "" && len(ev.rdates) == 0 {
		if _, ok := overridden[s0.Unix()]; ok || !x.overlaps(s0, span(s0)) {
			return nil
		}
		return x.emit(ev, s0, span(s0), time.Time{})
	}

	excluded := make(map[int64]bool, len(ev.exdates))
	for _, ex := range ev.exdates {
		excluded[ex.in(x.loc).Unix()] = true
	}
	next := x.instances(ev, s0)
	for scanned, emitted := 0, 0; scanned < x.lim.scanPerEvent && emitted < x.perEvent; {
		c, ok := next()
		if !ok || !c.Before(x.end) {
			return nil
		}
		scanned++
		if x.scanned++; x.scanned > x.lim.scanPerExpand {
			return fmt.Errorf("%w: more than %d recurrence instances scanned", ErrExpansionLimit, x.lim.scanPerExpand)
		}
		if excluded[c.Unix()] || overridden[c.Unix()] != nil {
			continue
		}
		if e := span(c); x.overlaps(c, e) {
			if err := x.emit(ev, c, e, c); err != nil {
				return err
			}
			emitted++
		}
	}
	return nil
}

// instances yields the master's recurrence set in ascending order without
// duplicates: DTSTART, the RRULE instances, and the RDATEs. A rule that
// fails validation here (e.g. a floating DTSTART shifted by a DST gap in
// the default location makes it unreachable) is dropped, as at parse time.
func (x *expander) instances(ev *vevent, s0 time.Time) func() (time.Time, bool) {
	extra := make([]time.Time, 0, 1+len(ev.rdates))
	extra = append(extra, s0)
	for _, rd := range ev.rdates {
		extra = append(extra, rd.in(x.loc))
	}
	slices.SortFunc(extra, time.Time.Compare)

	var ruleNext func() (time.Time, bool)
	if ev.rrule != "" {
		if r, err := buildRule(ev.rrule, s0, ev.start.date); err == nil {
			ruleNext = r.Iterator()
		}
	}
	return mergeAscending(ruleNext, extra)
}

// mergeAscending merges an ascending iterator (may be nil) with an
// ascending slice, dropping equal instants.
func mergeAscending(next func() (time.Time, bool), extra []time.Time) func() (time.Time, bool) {
	var (
		head           time.Time
		headOK, primed bool
		last           time.Time
		haveLast       bool
		i              int
	)
	return func() (time.Time, bool) {
		for {
			if next != nil && !primed {
				head, headOK = next()
				primed = true
			}
			var t time.Time
			switch {
			case next != nil && headOK && (i == len(extra) || !extra[i].Before(head)):
				t, primed = head, false
			case i < len(extra):
				t = extra[i]
				i++
			default:
				return time.Time{}, false
			}
			if haveLast && t.Equal(last) {
				continue
			}
			last, haveLast = t, true
			return t, true
		}
	}
}

// spanOf returns the function computing an instance's end from its start.
// All-day spans are whole days (local midnights, DST-safe); timed spans
// are the exact DTEND-DTSTART or DURATION, zero when neither is present.
func (x *expander) spanOf(ev *vevent, s0 time.Time) func(time.Time) time.Time {
	if ev.start.date {
		days := 1
		switch {
		case ev.end != nil:
			if d := daysBetween(ev.start.t, ev.end.t); d > 0 {
				days = d
			}
		case ev.duration != nil:
			if d := int(*ev.duration / (24 * time.Hour)); d > 0 {
				days = d
			}
		}
		return func(s time.Time) time.Time { return s.AddDate(0, 0, days) }
	}
	var dur time.Duration
	switch {
	case ev.end != nil:
		dur = ev.end.in(x.loc).Sub(s0)
	case ev.duration != nil:
		dur = *ev.duration
	}
	dur = max(dur, 0)
	return func(s time.Time) time.Time { return s.Add(dur) }
}

// daysBetween counts calendar days between the dates of a and b.
func daysBetween(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return int(time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC).Sub(time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)) / (24 * time.Hour))
}

func (x *expander) overlaps(s, e time.Time) bool {
	if !s.Before(x.end) {
		return false
	}
	if e.After(s) {
		return e.After(x.start)
	}
	return !s.Before(x.start)
}

func (x *expander) emit(ev *vevent, s, e, rid time.Time) error {
	if len(x.out) >= x.lim.occurrencesPerExpand {
		return fmt.Errorf("%w: more than %d occurrences", ErrExpansionLimit, x.lim.occurrencesPerExpand)
	}
	x.out = append(x.out, Occurrence{
		UID:          ev.uid,
		Start:        s,
		End:          e,
		AllDay:       ev.start.date,
		Title:        ev.summary,
		Description:  ev.description,
		Location:     ev.location,
		RecurrenceID: rid,
	})
	return nil
}
