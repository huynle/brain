package calendar

import (
	"fmt"
	"sort"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/pkg/marketcal"
)

// Source kinds.
const (
	KindBuiltin = "builtin"
	KindICS     = "ics"
)

// DayCalendar answers whether a civil date is a trading day.
type DayCalendar interface {
	IsOpen(d marketcal.Date) bool
}

// Registry names the configured calendar sources. It is built once from
// config and never mutated, so it is safe for concurrent use. Builtin sources
// are backed by pkg/marketcal; ics sources are known by name and kind only
// (their polling is a separate concern). A nil *Registry knows no calendars.
//
// The registry holds no URLs and never reports one.
type Registry struct {
	kinds map[string]string
	days  map[string]DayCalendar
}

// NewRegistry builds the registry from the configured sources. It returns an
// error naming the calendar when a source cannot be built. Config validation
// rejects the same problems first, so this guards callers that skip it.
func NewRegistry(calendars map[string]config.CalendarConfig) (*Registry, error) {
	names := make([]string, 0, len(calendars))
	for name := range calendars {
		names = append(names, name)
	}
	sort.Strings(names)

	r := &Registry{
		kinds: make(map[string]string, len(calendars)),
		days:  make(map[string]DayCalendar),
	}
	for _, name := range names {
		c := calendars[name]
		switch c.Type {
		case KindBuiltin:
			if c.Market != "XNYS" {
				return nil, fmt.Errorf("calendar %q: market must be XNYS (got %q)", name, c.Market)
			}
			xnys, err := marketcal.NewXNYS(marketcal.XNYSOptions{
				ExtraClosed: c.ExtraClosed,
				ExtraOpen:   c.ExtraOpen,
			})
			if err != nil {
				return nil, fmt.Errorf("calendar %q: %w", name, err)
			}
			r.days[name] = xnys
		case KindICS:
		default:
			return nil, fmt.Errorf("calendar %q: unknown type %q", name, c.Type)
		}
		r.kinds[name] = c.Type
	}
	return r, nil
}

// Kind returns the kind of a named source, and false when no source has that
// name. A nil registry knows no names.
func (r *Registry) Kind(name string) (string, bool) {
	if r == nil {
		return "", false
	}
	kind, ok := r.kinds[name]
	return kind, ok
}

// DayCalendar returns the trading-day calendar of a builtin source. It is
// false for an unknown name and for an ics source, which is not a day
// calendar. A nil registry knows no calendars.
func (r *Registry) DayCalendar(name string) (DayCalendar, bool) {
	if r == nil {
		return nil, false
	}
	day, ok := r.days[name]
	return day, ok
}
