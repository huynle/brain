// Package cron provides a 5-field cron expression parser and matcher.
// Supports standard format: minute hour dayOfMonth month dayOfWeek
//
// Times are evaluated in their own location (pass t.In(loc) to choose the
// zone). Day fields follow Vixie cron: when neither day-of-month nor
// day-of-week starts with '*', a day matches if EITHER matches; otherwise
// both must. Across DST, a local time that does not exist never matches,
// and a local time that occurs twice matches only at its first occurrence.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// fieldLimits defines the valid range for each cron field.
type fieldLimits struct {
	min int
	max int
}

var limits = [5]fieldLimits{
	{0, 59}, // minute
	{0, 23}, // hour
	{1, 31}, // day of month
	{1, 12}, // month
	{0, 7},  // day of week (0 and 7 = Sunday)
}

// Schedule represents a parsed cron expression.
type Schedule struct {
	fields [5]fieldSet

	// domStar and dowStar record whether the day-of-month and day-of-week
	// fields were written starting with '*' ("*", "*/2", ...). They decide
	// how the two day fields combine; see dayMatches.
	domStar bool
	dowStar bool
}

// fieldSet is a set of allowed values for a cron field.
type fieldSet struct {
	bits [64]bool // bit set for values 0-63 (covers all cron ranges)
}

func (fs *fieldSet) set(v int) {
	if v >= 0 && v < len(fs.bits) {
		fs.bits[v] = true
	}
}

func (fs *fieldSet) has(v int) bool {
	if v >= 0 && v < len(fs.bits) {
		return fs.bits[v]
	}
	return false
}

// Parse parses a 5-field cron expression into a Schedule.
func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty cron expression")
	}

	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(parts))
	}

	s := &Schedule{
		domStar: strings.HasPrefix(parts[2], "*"),
		dowStar: strings.HasPrefix(parts[4], "*"),
	}
	for i, part := range parts {
		if err := parseField(part, limits[i], &s.fields[i]); err != nil {
			return nil, fmt.Errorf("field %d (%s): %w", i, part, err)
		}
	}

	// Normalize day-of-week: map 7 to 0 (both mean Sunday)
	if s.fields[4].has(7) {
		s.fields[4].set(0)
	}

	return s, nil
}

// parseField parses a single cron field (e.g., "*/15", "9-17", "1,3,5").
func parseField(field string, lim fieldLimits, fs *fieldSet) error {
	// Handle comma-separated list
	for _, part := range strings.Split(field, ",") {
		if err := parseFieldPart(part, lim, fs); err != nil {
			return err
		}
	}
	return nil
}

// parseFieldPart parses a single part of a cron field (no commas).
func parseFieldPart(part string, lim fieldLimits, fs *fieldSet) error {
	// Check for step: "X/Y" or "*/Y" or "A-B/Y"
	var stepStr string
	if idx := strings.Index(part, "/"); idx >= 0 {
		stepStr = part[idx+1:]
		part = part[:idx]
	}

	var rangeStart, rangeEnd int

	if part == "*" {
		rangeStart = lim.min
		rangeEnd = lim.max
	} else if idx := strings.Index(part, "-"); idx >= 0 {
		// Range: A-B
		var err error
		rangeStart, err = strconv.Atoi(part[:idx])
		if err != nil {
			return fmt.Errorf("invalid range start %q: %w", part[:idx], err)
		}
		rangeEnd, err = strconv.Atoi(part[idx+1:])
		if err != nil {
			return fmt.Errorf("invalid range end %q: %w", part[idx+1:], err)
		}
		if rangeStart > rangeEnd {
			return fmt.Errorf("invalid range %d-%d: start > end", rangeStart, rangeEnd)
		}
	} else {
		// Single value
		v, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid value %q: %w", part, err)
		}
		rangeStart = v
		if stepStr != "" {
			// "V/S" means starting at V, step by S, up to max
			rangeEnd = lim.max
		} else {
			rangeEnd = v
		}
	}

	// Validate range bounds
	// For day-of-week, allow 7 (maps to 0 = Sunday)
	maxAllowed := lim.max
	if rangeStart < lim.min || rangeStart > maxAllowed {
		return fmt.Errorf("value %d out of range [%d-%d]", rangeStart, lim.min, maxAllowed)
	}
	if rangeEnd < lim.min || rangeEnd > maxAllowed {
		return fmt.Errorf("value %d out of range [%d-%d]", rangeEnd, lim.min, maxAllowed)
	}

	// Apply step
	step := 1
	if stepStr != "" {
		var err error
		step, err = strconv.Atoi(stepStr)
		if err != nil {
			return fmt.Errorf("invalid step %q: %w", stepStr, err)
		}
		if step <= 0 {
			return fmt.Errorf("step must be positive, got %d", step)
		}
	}

	for v := rangeStart; v <= rangeEnd; v += step {
		fs.set(v)
	}

	return nil
}

// Matches checks if a given time matches the cron schedule.
// The time is evaluated in its own location (use t.In(loc) to control timezone).
// For backward compatibility, UTC times behave as before.
//
// Seconds are ignored. The second pass through a local hour repeated by a
// DST fall-back never matches, so a schedule probed once a minute fires
// once per wall-clock slot, at its first occurrence (addendum decision #7).
func (s *Schedule) Matches(t time.Time) bool {
	minute := t.Minute()
	hour := t.Hour()
	day := t.Day()
	month := int(t.Month())
	weekday := int(t.Weekday()) // 0=Sunday

	return s.fields[0].has(minute) &&
		s.fields[1].has(hour) &&
		s.dayMatches(day, weekday) &&
		s.fields[3].has(month) &&
		!isRepeatedOccurrence(t)
}

// isRepeatedOccurrence reports whether t's local wall clock already
// occurred at an earlier instant — the second pass through an hour that a
// backward offset change (DST fall-back) repeats. A repeated wall time
// fires once, at its first occurrence.
func isRepeatedOccurrence(t time.Time) bool {
	_, ok := earlierOccurrence(t)
	return ok
}

// earlierOccurrence returns an earlier instant showing exactly t's local
// wall clock, if one exists.
//
// An earlier instant u with the same wall clock satisfies
// u + offset(u) = t + offset(t), so u = t - (offset(u) - offset(t)) for an
// offset larger than t's. The candidates are the offsets of the zone
// periods just before t's; the wall-clock comparison is the proof, so a
// candidate from the wrong period is simply rejected. Two periods are
// examined so a metadata-only transition (same offset, new abbreviation)
// sitting inside a repeated span does not hide it.
func earlierOccurrence(t time.Time) (time.Time, bool) {
	_, off := t.Zone()
	p := t
	for i := 0; i < 2; i++ {
		start, _ := p.ZoneBounds()
		if start.IsZero() {
			break // fixed zone, or no earlier transition
		}
		prev := start.Add(-time.Nanosecond)
		if _, prevOff := prev.Zone(); prevOff > off {
			u := t.Add(-time.Duration(prevOff-off) * time.Second)
			if sameWallClock(u, t) {
				return u, true
			}
		}
		p = prev
	}
	return time.Time{}, false
}

// firstOccurrence maps t to the earliest instant showing the same local wall
// clock. time.Date leaves the choice unspecified for a repeated wall time —
// in practice it returns the first pass in America/New_York but the second
// in Europe/London — so every constructed candidate goes through here.
func firstOccurrence(t time.Time) time.Time {
	for i := 0; i < 4; i++ {
		u, ok := earlierOccurrence(t)
		if !ok {
			break
		}
		t = u
	}
	return t
}

// sameWallClock reports whether a and b show the same local date and time
// of day, to the second.
func sameWallClock(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	ah, ami, as := a.Clock()
	bh, bmi, bs := b.Clock()
	return ay == by && am == bm && ad == bd && ah == bh && ami == bmi && as == bs
}

// dayMatches is the single day predicate shared by matching and searching.
//
// Vixie cron rule: when neither day field starts with '*', a day qualifies
// if it matches day-of-month OR day-of-week ("0 3 1 * 1" runs on the 1st
// and on every Monday). Otherwise both must match; an unrestricted "*"
// field matches every day, so that reduces to the restricted field alone.
// A stepped star such as "*/2" still counts as starred and ANDs.
func (s *Schedule) dayMatches(day, weekday int) bool {
	dom := s.fields[2].has(day)
	dow := s.fields[4].has(weekday)
	if s.domStar || s.dowStar {
		return dom && dow
	}
	return dom || dow
}

// DayFieldsBothRestricted reports whether neither the day-of-month nor the
// day-of-week field starts with '*' — the expressions whose day fields
// combine with OR rather than AND.
func (s *Schedule) DayFieldsBothRestricted() bool {
	return !s.domStar && !s.dowStar
}
