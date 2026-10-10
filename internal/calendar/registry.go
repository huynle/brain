package calendar

import (
	"fmt"
	"sort"
	"sync"
	"time"

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

// Status is the observable state of one configured calendar source. Builtin
// sources report Kind builtin and no fetch fields. A Status never carries a
// feed URL or a file path, and LastError is already stripped of both.
type Status struct {
	Name        string
	Kind        string
	LastFetch   time.Time
	LastSuccess time.Time
	EventCount  int
	LastError   string
	Stale       bool
}

// Registry names the configured calendar sources. The set of names and their
// kinds is built once from config and never changes. The poller publishes
// each ics source's latest snapshot here, so reads are guarded by mu. The
// registry holds no URLs and never reports one. A nil *Registry knows no
// calendars.
type Registry struct {
	kinds map[string]string
	days  map[string]DayCalendar

	mu    sync.RWMutex
	feeds map[string]snapshot
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
		feeds: make(map[string]snapshot),
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

// Occurrences returns a copy of the latest expanded occurrences of a source,
// the source's status, and whether the name is a configured source at all.
// A builtin source is known, with no occurrences. An ics source that has
// never been fetched is known, with none yet. A nil registry knows nothing.
func (r *Registry) Occurrences(name string) ([]Occurrence, Status, bool) {
	if r == nil {
		return nil, Status{}, false
	}
	kind, known := r.kinds[name]
	if !known {
		return nil, Status{}, false
	}
	if kind == KindBuiltin {
		return nil, Status{Name: name, Kind: KindBuiltin}, true
	}
	snap, ok := r.snapshotOf(name)
	if !ok {
		return nil, Status{Name: name, Kind: KindICS}, true
	}
	occ := make([]Occurrence, len(snap.Occurrences))
	copy(occ, snap.Occurrences)
	return occ, statusOf(name, kind, snap), true
}

// Statuses returns the status of every configured source, sorted by name.
// A nil registry returns none.
func (r *Registry) Statuses() []Status {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.kinds))
	for name := range r.kinds {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Status, 0, len(names))
	for _, name := range names {
		kind := r.kinds[name]
		if kind == KindBuiltin {
			out = append(out, Status{Name: name, Kind: KindBuiltin})
			continue
		}
		snap, _ := r.snapshotOf(name)
		out = append(out, statusOf(name, kind, snap))
	}
	return out
}

// statusOf derives the public status of a source from its snapshot. A source
// with no snapshot yet has zero fetch fields.
func statusOf(name, kind string, snap snapshot) Status {
	return Status{
		Name:        name,
		Kind:        kind,
		LastFetch:   snap.LastFetch,
		LastSuccess: snap.LastSuccess,
		EventCount:  len(snap.Occurrences),
		LastError:   snap.LastError,
		Stale:       snap.Stale,
	}
}

// setSnapshot publishes an ics source's latest snapshot. The poller is the
// only caller. The registry copies what it keeps, so later changes to snap's
// slices never reach readers.
func (r *Registry) setSnapshot(snap snapshot) {
	cp := snap
	cp.Occurrences = make([]Occurrence, len(snap.Occurrences))
	copy(cp.Occurrences, snap.Occurrences)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.feeds == nil {
		r.feeds = make(map[string]snapshot)
	}
	r.feeds[snap.Name] = cp
}

// snapshotOf returns the last published snapshot of an ics source.
func (r *Registry) snapshotOf(name string) (snapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap, ok := r.feeds[name]
	return snap, ok
}

// Window returns the time span the source's latest snapshot covers: the
// window of its last full (200) fetch, [start, end). The poller expands
// [poll − windowBefore, poll + windowAfter), so the start is derived from the
// recorded end with the same constants. ok is false for a name that is not an
// ics source, and for a source that has never completed a full fetch. A nil
// registry knows no windows.
func (r *Registry) Window(name string) (start, end time.Time, ok bool) {
	if r == nil {
		return time.Time{}, time.Time{}, false
	}
	if kind, known := r.kinds[name]; !known || kind != KindICS {
		return time.Time{}, time.Time{}, false
	}
	snap, found := r.snapshotOf(name)
	if !found || snap.WindowEnd.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	return snap.WindowEnd.Add(-(windowBefore + windowAfter)), snap.WindowEnd, true
}
