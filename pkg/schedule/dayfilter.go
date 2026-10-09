package schedule

import (
	"context"
	"time"
)

// DayFilter decides whether slots may fall on a local calendar day: a
// market calendar, "skip if an event is on my calendar", and so on.
//
// day is the start of the base slot's local date in the schedule's
// location — local midnight, or the first instant after it when a DST
// change skips midnight. Filters should look only at day.Date(). The date
// is the base slot's, before the stagger offset is added (addendum decision
// #8), so a slot staggered past midnight is still judged by its own day.
//
// A rejected day is skipped, never shifted: its slots simply do not exist.
// An error aborts the search and is returned to the caller unchanged.
type DayFilter interface {
	Allowed(ctx context.Context, day time.Time) (bool, error)
}

// DayFilterFunc adapts a function to DayFilter.
type DayFilterFunc func(ctx context.Context, day time.Time) (bool, error)

// Allowed calls f.
func (f DayFilterFunc) Allowed(ctx context.Context, day time.Time) (bool, error) {
	return f(ctx, day)
}
