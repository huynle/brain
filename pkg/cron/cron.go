// Package cron provides a 5-field cron expression parser and matcher.
// Supports standard format: minute hour dayOfMonth month dayOfWeek
// All time evaluation uses UTC.
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

	s := &Schedule{}
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
func (s *Schedule) Matches(t time.Time) bool {
	minute := t.Minute()
	hour := t.Hour()
	day := t.Day()
	month := int(t.Month())
	weekday := int(t.Weekday()) // 0=Sunday

	return s.fields[0].has(minute) &&
		s.fields[1].has(hour) &&
		s.fields[2].has(day) &&
		s.fields[3].has(month) &&
		s.fields[4].has(weekday)
}

// NextAfter returns the next time after t that matches the schedule.
// Searches up to 1 year ahead. Returns zero time if no match found.
// The returned time preserves the location of the input time.
// Seconds and nanoseconds are zeroed.
func (s *Schedule) NextAfter(t time.Time) time.Time {
	loc := t.Location()

	// Start from the next minute
	candidate := t.Truncate(time.Minute).Add(time.Minute)

	// Search up to ~366 days ahead (527040 minutes)
	maxIterations := 527040
	for i := 0; i < maxIterations; i++ {
		if s.Matches(candidate) {
			return candidate
		}

		// Smart advancement: skip ahead when possible
		candidate = s.advanceCandidate(candidate, loc)
	}

	return time.Time{}
}

// advanceCandidate tries to skip ahead intelligently rather than
// incrementing one minute at a time.
func (s *Schedule) advanceCandidate(t time.Time, loc *time.Location) time.Time {
	// Check month first (biggest skip)
	month := int(t.Month())
	if !s.fields[3].has(month) {
		// Skip to next valid month
		for m := month + 1; m <= 12; m++ {
			if s.fields[3].has(m) {
				return time.Date(t.Year(), time.Month(m), 1, 0, 0, 0, 0, loc)
			}
		}
		// Wrap to next year
		for m := 1; m <= 12; m++ {
			if s.fields[3].has(m) {
				return time.Date(t.Year()+1, time.Month(m), 1, 0, 0, 0, 0, loc)
			}
		}
	}

	// Check day of month and day of week
	day := t.Day()
	weekday := int(t.Weekday())
	if !s.fields[2].has(day) || !s.fields[4].has(weekday) {
		// Skip to next day
		next := t.AddDate(0, 0, 1)
		return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)
	}

	// Check hour
	hour := t.Hour()
	if !s.fields[1].has(hour) {
		// Skip to next valid hour today
		for h := hour + 1; h <= 23; h++ {
			if s.fields[1].has(h) {
				return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, loc)
			}
		}
		// Wrap to next day
		next := t.AddDate(0, 0, 1)
		return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)
	}

	// Minute doesn't match — just advance by 1 minute
	return t.Add(time.Minute)
}
