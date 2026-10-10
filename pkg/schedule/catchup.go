package schedule

import (
	"fmt"
	"time"
)

// tick is the granularity callers evaluate schedules at. A slot is "on
// time" when it is seen within one tick, so no lateness cap is shorter.
const tick = time.Minute

// CatchUp bounds how late a slot may still fire. The zero value is the
// default: the latest missed slot fires however late it is.
type CatchUp struct {
	capped bool
	none   bool
	max    time.Duration
}

// ParseCatchUp parses a trigger's catch_up value: "" for the default (any
// lateness), "none" for on-time only, or a non-negative Go duration capping
// the lateness. A cap below one tick behaves as one tick, because a minute
// ticker never sees a slot sooner than that.
func ParseCatchUp(s string) (CatchUp, error) {
	switch s {
	case "":
		return CatchUp{}, nil
	case "none":
		return CatchUp{capped: true, none: true}, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return CatchUp{}, fmt.Errorf("catch_up %q: want \"none\" or a duration such as \"10m\"", s)
	}
	if d < 0 {
		return CatchUp{}, fmt.Errorf("catch_up %q: must not be negative", s)
	}
	return CatchUp{capped: true, max: d}, nil
}

// Allows reports whether a slot at slotAt may fire at now: it must be due,
// and when capped, less than max(cap, one tick) late.
func (c CatchUp) Allows(slotAt, now time.Time) bool {
	if now.Before(slotAt) {
		return false
	}
	if !c.capped {
		return true
	}
	limit := c.max
	if limit < tick {
		limit = tick
	}
	return now.Sub(slotAt) < limit
}

// String returns the value ParseCatchUp would accept for c.
func (c CatchUp) String() string {
	switch {
	case !c.capped:
		return ""
	case c.none:
		return "none"
	default:
		return c.max.String()
	}
}

// Due reports whether slot should fire at now: it must be newer than floor
// (the latest instant already handled or otherwise excluded — last handled
// slot, edit times, starts_at) and allowed by the catch-up policy.
func Due(slot Slot, floor, now time.Time, c CatchUp) bool {
	return slot.At.After(floor) && c.Allows(slot.At, now)
}
