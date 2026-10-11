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

// Source origins: where a source was defined. Config sources come from
// server.calendars and are immutable through the API. Runtime sources were
// added through the admin API and live in the source store.
const (
	OriginConfig  = "config"
	OriginRuntime = "runtime"
)

// DayCalendar answers whether a civil date is a trading day.
type DayCalendar interface {
	IsOpen(d marketcal.Date) bool
}

// Status is the observable state of one calendar source. Builtin sources
// report Kind builtin and no fetch fields. Poll is the ics poll interval and
// is zero for builtin sources. A Status never carries a feed URL or a file
// path, and LastError is already stripped of both.
type Status struct {
	Name        string
	Kind        string
	Origin      string
	Poll        time.Duration
	LastFetch   time.Time
	LastSuccess time.Time
	EventCount  int
	LastError   string
	Stale       bool
}

// Registry names the calendar sources. Config sources are built once from
// config. Runtime ics sources are added and removed while the server runs, so
// every mutable map is guarded by mu. The poller publishes each ics source's
// latest snapshot here. The registry holds no URLs and never reports one. A
// nil *Registry knows no calendars.
type Registry struct {
	// days holds the builtin day calendars. It is written only by
	// NewRegistry and is read without the lock.
	days map[string]DayCalendar

	mu      sync.RWMutex
	kinds   map[string]string
	origins map[string]string
	polls   map[string]time.Duration
	feeds   map[string]snapshot
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
		days:    make(map[string]DayCalendar),
		kinds:   make(map[string]string, len(calendars)),
		origins: make(map[string]string, len(calendars)),
		polls:   make(map[string]time.Duration),
		feeds:   make(map[string]snapshot),
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
			r.polls[name] = PollInterval(c)
		default:
			return nil, fmt.Errorf("calendar %q: unknown type %q", name, c.Type)
		}
		r.kinds[name] = c.Type
		r.origins[name] = OriginConfig
	}
	return r, nil
}

// Kind returns the kind of a named source, and false when no source has that
// name. A nil registry knows no names.
func (r *Registry) Kind(name string) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	kind, ok := r.kinds[name]
	return kind, ok
}

// Origin returns where a named source was defined, and false when no source
// has that name. A nil registry knows no names.
func (r *Registry) Origin(name string) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	origin, ok := r.origins[name]
	return origin, ok
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
// the source's status, and whether the name is a known source at all. A
// builtin source is known, with no occurrences. An ics source that has never
// been fetched is known, with none yet. A nil registry knows nothing.
func (r *Registry) Occurrences(name string) ([]Occurrence, Status, bool) {
	if r == nil {
		return nil, Status{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	kind, known := r.kinds[name]
	if !known {
		return nil, Status{}, false
	}
	if kind == KindBuiltin {
		return nil, r.statusLocked(name), true
	}
	snap, ok := r.feeds[name]
	if !ok {
		return nil, r.statusLocked(name), true
	}
	occ := make([]Occurrence, len(snap.Occurrences))
	copy(occ, snap.Occurrences)
	return occ, r.statusLocked(name), true
}

// Statuses returns the status of every known source, sorted by name. A nil
// registry returns none.
func (r *Registry) Statuses() []Status {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.kinds))
	for name := range r.kinds {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Status, 0, len(names))
	for _, name := range names {
		out = append(out, r.statusLocked(name))
	}
	return out
}

// statusLocked derives the public status of a known source. The caller holds
// mu. A source with no snapshot yet has zero fetch fields.
func (r *Registry) statusLocked(name string) Status {
	kind := r.kinds[name]
	s := Status{
		Name:   name,
		Kind:   kind,
		Origin: r.origins[name],
	}
	if kind == KindBuiltin {
		return s
	}
	s.Poll = r.polls[name]
	snap := r.feeds[name]
	s.LastFetch = snap.LastFetch
	s.LastSuccess = snap.LastSuccess
	s.EventCount = len(snap.Occurrences)
	s.LastError = snap.LastError
	s.Stale = snap.Stale
	return s
}

// setSnapshot publishes an ics source's latest snapshot. The poller is the
// only caller. The registry copies what it keeps, so later changes to snap's
// slices never reach readers. A snapshot for a name the registry does not
// know is dropped, so a poll that races a removal cannot resurrect it.
func (r *Registry) setSnapshot(snap snapshot) {
	cp := snap
	cp.Occurrences = make([]Occurrence, len(snap.Occurrences))
	copy(cp.Occurrences, snap.Occurrences)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, known := r.kinds[snap.Name]; !known {
		return
	}
	r.feeds[snap.Name] = cp
}

// addICS registers a new ics source with its origin and poll interval. It
// fails when any source already has the name, so a runtime source can never
// shadow a config source.
func (r *Registry) addICS(name, origin string, poll time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.kinds[name]; exists {
		return fmt.Errorf("calendar %q already exists", name)
	}
	r.kinds[name] = KindICS
	r.origins[name] = origin
	r.polls[name] = poll
	return nil
}

// removeICS unregisters a runtime ics source and its snapshot. It never
// touches a config source.
func (r *Registry) removeICS(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.origins[name] != OriginRuntime {
		return
	}
	delete(r.kinds, name)
	delete(r.origins, name)
	delete(r.polls, name)
	delete(r.feeds, name)
}

// setPoll changes the reported poll interval of an ics source.
func (r *Registry) setPoll(name string, poll time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.kinds[name] == KindICS {
		r.polls[name] = poll
	}
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if kind, known := r.kinds[name]; !known || kind != KindICS {
		return time.Time{}, time.Time{}, false
	}
	snap, found := r.feeds[name]
	if !found || snap.WindowEnd.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	return snap.WindowEnd.Add(-(windowBefore + windowAfter)), snap.WindowEnd, true
}
