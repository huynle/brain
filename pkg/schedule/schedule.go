// Package schedule decides when recurring work runs.
//
// A schedule yields slots: the instants at which work is due. A Spec names
// either a cron expression or an interval (every, optionally at a time of
// day), a timezone, a stagger window, an anchor and day filters; Compile
// validates it once, and LatestSlot and NextSlot answer "what was the last
// slot" and "what is the next one" for one target, whose stagger offset the
// caller supplies (see StaggerOffset). Due and CatchUp decide whether a
// slot should still fire.
//
// The package is pure: no I/O, no clock, no global state. Day filters are
// the only callbacks, and their errors are returned unchanged.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huynle/brain-api/pkg/cron"
)

// Spec describes a schedule in the terms an automation trigger uses.
type Spec struct {
	// Cron is a 5-field cron expression evaluated in Timezone. Exactly one
	// of Cron and Every must be set.
	Cron string
	// Every is an interval in the grammar <positive integer><m|h|d|w>; see
	// ParseEvery.
	Every string
	// At is a 24-hour "HH:MM" local time of day. Only valid with an Every
	// in days or weeks; empty means the Anchor's local wall time.
	At string
	// Timezone is an IANA zone name. Empty or invalid means UTC (an
	// invalid name is logged by cron.LoadTimezone).
	Timezone string
	// Stagger is the window per-target offsets are drawn from; see Offset.
	// Zero disables staggering.
	Stagger time.Duration
	// Anchor is where an Every schedule starts: starts_at, else the
	// entry's creation instant. Required with Every, ignored for Cron.
	Anchor time.Time
	// DayFilters must all allow a base slot's local date for the slot to
	// exist.
	DayFilters []DayFilter
}

// FieldError reports the Spec field that failed validation. Field uses
// the lower-case Spec field name: cron, every, at, anchor, stagger or
// day_filters.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Err.Error() }

// Unwrap returns the underlying error.
func (e *FieldError) Unwrap() error { return e.Err }

func fieldErr(field string, err error) error { return &FieldError{Field: field, Err: err} }

// Schedule is a compiled Spec. It is immutable and safe for concurrent use
// (provided its DayFilters are).
type Schedule struct {
	loc     *time.Location
	stagger time.Duration
	filters []DayFilter

	cron   *cron.Schedule
	every  Interval
	hasAt  bool
	atHour int
	atMin  int
	anchor time.Time
}

// Compile validates spec and returns its Schedule. Errors are *FieldError.
func Compile(spec Spec) (*Schedule, error) {
	if spec.At != "" && spec.Every == "" {
		return nil, fieldErr("at", errors.New("only valid with every"))
	}
	s := &Schedule{
		loc:     cron.LoadTimezone(spec.Timezone),
		stagger: spec.Stagger,
	}
	switch {
	case spec.Cron == "" && spec.Every == "":
		return nil, fieldErr("cron", errors.New("one of cron or every is required"))
	case spec.Cron != "" && spec.Every != "":
		return nil, fieldErr("every", errors.New("cannot be combined with cron"))
	case spec.Cron != "":
		c, err := cron.Parse(spec.Cron)
		if err != nil {
			return nil, fieldErr("cron", err)
		}
		s.cron = c
	default:
		if err := s.compileEvery(spec); err != nil {
			return nil, err
		}
	}
	if spec.Stagger < 0 {
		return nil, fieldErr("stagger", fmt.Errorf("must not be negative, got %v", spec.Stagger))
	}
	for i, f := range spec.DayFilters {
		if f == nil {
			return nil, fieldErr("day_filters", fmt.Errorf("filter %d is nil", i))
		}
	}
	s.filters = append([]DayFilter(nil), spec.DayFilters...)
	return s, nil
}

// compileEvery fills in the interval fields of s.
func (s *Schedule) compileEvery(spec Spec) error {
	iv, err := ParseEvery(spec.Every)
	if err != nil {
		return fieldErr("every", err)
	}
	s.every = iv
	if spec.At != "" {
		if !iv.Calendar() {
			return fieldErr("at", fmt.Errorf("only valid with every in days (d) or weeks (w), not %q", spec.Every))
		}
		h, m, err := ParseAt(spec.At)
		if err != nil {
			return fieldErr("at", err)
		}
		s.hasAt, s.atHour, s.atMin = true, h, m
	}
	if spec.Anchor.IsZero() {
		return fieldErr("anchor", errors.New("required with every"))
	}
	s.anchor = spec.Anchor
	return nil
}

// Location is the zone the schedule is evaluated in.
func (s *Schedule) Location() *time.Location { return s.loc }

// Stagger is the window per-target offsets are drawn from.
func (s *Schedule) Stagger() time.Duration { return s.stagger }

// Offset is StaggerOffset(automationID, project, s.Stagger()): the offset
// to pass to LatestSlot and NextSlot for that target.
func (s *Schedule) Offset(automationID, project string) time.Duration {
	return StaggerOffset(automationID, project, s.stagger)
}

// Slot is one scheduled instant for one target.
type Slot struct {
	// Base is the schedule's own instant — the cron match or interval
	// step — before the target's stagger offset.
	Base time.Time
	// At is Base plus the offset: when the target's run is due.
	At time.Time
}

// LatestSlot returns the latest slot with At <= now for the target whose
// stagger offset is offset.
func (s *Schedule) LatestSlot(ctx context.Context, now time.Time, offset time.Duration) (Slot, bool, error) {
	base, ok := s.prevBase(now.Add(-offset))
	if !ok {
		return Slot{}, false, nil
	}
	return Slot{Base: base, At: base.Add(offset)}, true, nil
}

// NextSlot returns the earliest slot with At > after for the target whose
// stagger offset is offset.
func (s *Schedule) NextSlot(ctx context.Context, after time.Time, offset time.Duration) (Slot, bool, error) {
	base, ok := s.nextBase(after.Add(-offset))
	if !ok {
		return Slot{}, false, nil
	}
	return Slot{Base: base, At: base.Add(offset)}, true, nil
}

// prevBase returns the latest base instant at or before t.
func (s *Schedule) prevBase(t time.Time) (time.Time, bool) {
	b := s.cron.PrevAtOrBefore(t.In(s.loc))
	return b, !b.IsZero()
}

// nextBase returns the earliest base instant after t.
func (s *Schedule) nextBase(t time.Time) (time.Time, bool) {
	b := s.cron.NextAfter(t.In(s.loc))
	return b, !b.IsZero()
}
