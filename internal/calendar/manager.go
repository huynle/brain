package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Sentinel errors. The API maps each to a status code with errors.Is. Errors
// that wrap one carry a redacted reason and never the address.
var (
	// ErrInvalidName is a name outside ^[a-z0-9][a-z0-9_-]{0,63}$.
	ErrInvalidName = errors.New("calendar name is invalid")
	// ErrInvalidPoll is a poll interval below one minute.
	ErrInvalidPoll = errors.New("calendar poll is invalid")
	// ErrExists is a name that any source already has.
	ErrExists = errors.New("calendar already exists")
	// ErrLimit is the runtime source limit being reached.
	ErrLimit = errors.New("calendar source limit reached")
	// ErrNotFound is a name no source has.
	ErrNotFound = errors.New("calendar not found")
	// ErrConfigSource is a write to a source defined in server config.
	ErrConfigSource = errors.New("calendar is defined in server config")
	// ErrRejectedURL is an address that is malformed, not public, or that did
	// not return a parseable feed.
	ErrRejectedURL = errors.New("calendar address rejected")
	// ErrNothingToUpdate is an update that sets neither the address nor the poll.
	ErrNothingToUpdate = errors.New("nothing to update: set url or poll")
)

// defaultMaxSources is the runtime source limit when ManagerOptions leaves it
// at zero.
const defaultMaxSources = 32

// Reference is one automation field that names a calendar. Field is one of
// trigger.calendar, trigger.skip_if_event or trigger.only_if_event.
type Reference struct{ ID, Path, Title, Field string }

// ReferenceFinder lists the automation fields that name a calendar. It scans
// every automation status, bindings included. A nil finder means no source is
// ever referenced, so removal is never blocked.
type ReferenceFinder func(ctx context.Context, name string) ([]Reference, error)

// ManagerOptions tunes a Manager. The zero value is production behaviour.
type ManagerOptions struct {
	// References reports the automations that name a calendar.
	References ReferenceFinder
	// MaxSources caps runtime sources. Zero means 32.
	MaxSources int
	// Clock returns the current time. Nil means time.Now.
	Clock func() time.Time
	// Logger receives one line per write. Nil means slog.Default. Lines carry
	// the source name and the caller's name, never an address.
	Logger *slog.Logger

	// hooks are the test seams of runtime clients. Production leaves them zero.
	hooks runtimeHooks
}

// AddRequest creates a runtime source. Poll zero means five minutes.
type AddRequest struct {
	Name, URL string
	Poll      time.Duration
}

// UpdateRequest changes a runtime source. At least one field must be set.
type UpdateRequest struct {
	URL  *string
	Poll *time.Duration
}

// RemoveResult reports the automations that named a removed source.
type RemoveResult struct {
	References []Reference
}

// ReferencedError is returned by Remove when a source is still referenced and
// force is not set. errors.As extracts the references.
type ReferencedError struct {
	References []Reference
}

func (e *ReferencedError) Error() string {
	return fmt.Sprintf("calendar is referenced by %d automation field(s); remove with force to proceed", len(e.References))
}

// Manager is the one owner of runtime calendar sources. It validates each
// write, persists it, and applies it to the registry and poller. All writes
// are serialized by mu, so a write is never half applied.
type Manager struct {
	mu sync.Mutex

	reg     *Registry
	poller  *Poller
	store   *sourceStore
	refs    ReferenceFinder
	max     int
	clock   func() time.Time
	logger  *slog.Logger
	hooks   runtimeHooks
	timeout time.Duration

	// sources holds the runtime sources, by name, with their clients.
	sources map[string]*runtimeSource
}

// runtimeSource is the manager's record of one runtime source and the HTTP
// client it fetches through.
type runtimeSource struct {
	rec    sourceRecord
	client *http.Client
}

// NewManager loads the runtime sources saved under dataDir and attaches them
// to reg and poller. Config sources are not touched. A source file that is
// corrupt or unreadable, that collides with a config source, or that is over
// the limit is skipped and logged, never fatal. Sources load with an immediate
// first poll, like config sources.
func NewManager(reg *Registry, poller *Poller, dataDir string, opts ManagerOptions) (*Manager, error) {
	if reg == nil || poller == nil {
		return nil, errors.New("calendar manager requires a registry and a poller")
	}
	if opts.MaxSources < 0 {
		return nil, errors.New("calendar manager: MaxSources must not be negative")
	}
	max := opts.MaxSources
	if max == 0 {
		max = defaultMaxSources
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		reg:     reg,
		poller:  poller,
		store:   newSourceStore(dataDir),
		refs:    opts.References,
		max:     max,
		clock:   clock,
		logger:  logger,
		hooks:   opts.hooks,
		timeout: poller.timeout,
		sources: make(map[string]*runtimeSource),
	}
	poller.markManaged()

	records, issues := m.store.loadAll()
	for _, is := range issues {
		logger.Warn("calendar source file skipped", "file", is.File, "reason", is.Reason)
	}
	for _, rec := range records {
		if _, taken := reg.Kind(rec.Name); taken {
			logger.Warn("calendar source skipped: name is defined in server config", "calendar", rec.Name)
			continue
		}
		if len(m.sources) >= m.max {
			logger.Warn("calendar source skipped: runtime source limit reached", "calendar", rec.Name)
			continue
		}
		client := newRuntimeClient(m.hooks, m.timeout)
		if err := reg.addICS(rec.Name, OriginRuntime, rec.Poll); err != nil {
			client.CloseIdleConnections()
			logger.Warn("calendar source skipped", "calendar", rec.Name, "reason", "registry refused it")
			continue
		}
		snap, found, err := poller.store.load(rec.Name)
		if err != nil {
			logger.Warn("calendar snapshot unreadable; starting fresh", "calendar", rec.Name)
			found = false
		}
		if !found {
			snap = snapshot{}
		}
		if err := poller.attachRuntime(rec.Name, rec.URL, rec.Poll, client, snap, 0); err != nil {
			reg.removeICS(rec.Name)
			client.CloseIdleConnections()
			logger.Warn("calendar source skipped", "calendar", rec.Name, "reason", "poller refused it")
			continue
		}
		m.sources[rec.Name] = &runtimeSource{rec: rec, client: client}
	}
	return m, nil
}

// Add validates a new runtime source, fetches its feed once, and only then
// saves, registers and starts polling it. Validation runs in this order:
// name, poll, uniqueness, limit, address shape, then the fetch and parse. A
// failure at any step saves nothing.
func (m *Manager) Add(ctx context.Context, req AddRequest, actor string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !validRuntimeName(req.Name) {
		return Status{}, fmt.Errorf("%w: must match ^[a-z0-9][a-z0-9_-]{0,63}$", ErrInvalidName)
	}
	poll, err := normalizePoll(req.Poll)
	if err != nil {
		return Status{}, err
	}
	if _, exists := m.reg.Kind(req.Name); exists {
		return Status{}, fmt.Errorf("%w: %s", ErrExists, req.Name)
	}
	if len(m.sources) >= m.max {
		return Status{}, fmt.Errorf("%w: at most %d runtime sources", ErrLimit, m.max)
	}
	if err := validateAddress(req.URL); err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrRejectedURL, err)
	}

	now := m.clock()
	client := newRuntimeClient(m.hooks, m.timeout)
	snap, err := m.probe(ctx, req.Name, req.URL, client, now)
	if err != nil {
		client.CloseIdleConnections()
		return Status{}, err
	}

	rec := sourceRecord{Name: req.Name, URL: req.URL, Poll: poll, CreatedAt: now, UpdatedAt: now}
	if err := m.store.save(rec); err != nil {
		client.CloseIdleConnections()
		return Status{}, fmt.Errorf("save calendar source: %w", err)
	}
	if err := m.reg.addICS(req.Name, OriginRuntime, poll); err != nil {
		_ = m.store.remove(req.Name)
		client.CloseIdleConnections()
		return Status{}, fmt.Errorf("%w: %s", ErrExists, req.Name)
	}
	if err := m.poller.attachRuntime(req.Name, req.URL, poll, client, snap, poll); err != nil {
		m.reg.removeICS(req.Name)
		_ = m.store.remove(req.Name)
		client.CloseIdleConnections()
		return Status{}, fmt.Errorf("start calendar polling: %w", err)
	}
	m.sources[req.Name] = &runtimeSource{rec: rec, client: client}
	m.logger.Info("calendar source added", "calendar", req.Name, "actor", actor)
	return m.statusOf(req.Name), nil
}

// Update changes a runtime source's address or poll. A new address is
// validated with one fetch before anything changes. A poll-only update makes
// no request. A rejected update changes nothing.
func (m *Manager) Update(ctx context.Context, name string, req UpdateRequest, actor string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if req.URL == nil && req.Poll == nil {
		return Status{}, ErrNothingToUpdate
	}
	origin, known := m.reg.Origin(name)
	if !known {
		return Status{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	src, ok := m.sources[name]
	if origin != OriginRuntime || !ok {
		return Status{}, fmt.Errorf("%w: %s", ErrConfigSource, name)
	}

	updated := src.rec
	if req.Poll != nil {
		poll, err := normalizePoll(*req.Poll)
		if err != nil {
			return Status{}, err
		}
		updated.Poll = poll
	}
	now := m.clock()
	var fresh *snapshot
	if req.URL != nil {
		if err := validateAddress(*req.URL); err != nil {
			return Status{}, fmt.Errorf("%w: %v", ErrRejectedURL, err)
		}
		snap, err := m.probe(ctx, name, *req.URL, src.client, now)
		if err != nil {
			return Status{}, err
		}
		fresh = &snap
		updated.URL = *req.URL
	}
	updated.UpdatedAt = now

	if err := m.store.save(updated); err != nil {
		return Status{}, fmt.Errorf("save calendar source: %w", err)
	}
	if req.Poll != nil {
		m.reg.setPoll(name, updated.Poll)
		if err := m.poller.setRuntimePoll(name, updated.Poll); err != nil {
			return Status{}, fmt.Errorf("apply calendar poll: %w", err)
		}
	}
	if fresh != nil {
		if err := m.poller.setAddress(name, updated.URL, *fresh); err != nil {
			return Status{}, fmt.Errorf("apply calendar address: %w", err)
		}
	}
	src.rec = updated
	m.logger.Info("calendar source updated", "calendar", name, "actor", actor)
	return m.statusOf(name), nil
}

// Remove deletes a runtime source. A source that automations still reference
// is kept unless force is set; the references are returned either way. A
// removal stops polling first, then unregisters the source and deletes its
// file and cached snapshot.
func (m *Manager) Remove(ctx context.Context, name string, force bool, actor string) (RemoveResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	origin, known := m.reg.Origin(name)
	if !known {
		return RemoveResult{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	src, ok := m.sources[name]
	if origin != OriginRuntime || !ok {
		return RemoveResult{}, fmt.Errorf("%w: %s", ErrConfigSource, name)
	}

	refs, err := m.references(ctx, name)
	if err != nil {
		return RemoveResult{}, fmt.Errorf("check calendar references: %w", err)
	}
	result := RemoveResult{References: refs}
	if len(refs) > 0 && !force {
		return result, &ReferencedError{References: refs}
	}

	if err := m.store.remove(name); err != nil {
		return result, fmt.Errorf("delete calendar source: %w", err)
	}
	m.poller.detach(name)
	m.reg.removeICS(name)
	if err := m.poller.store.remove(name); err != nil {
		m.logger.Warn("calendar snapshot not deleted", "calendar", name, "error", err.Error())
	}
	src.client.CloseIdleConnections()
	delete(m.sources, name)
	m.logger.Info("calendar source removed", "calendar", name, "actor", actor)
	return result, nil
}

// probe fetches and parses an address once, through the source's guarded
// client, and returns the snapshot it would publish. Its error is always an
// ErrRejectedURL with a redacted reason, so nothing is saved on failure.
func (m *Manager) probe(ctx context.Context, name, address string, client *http.Client, now time.Time) (snapshot, error) {
	outcome, err := m.poller.fetch(ctx, client, name, address, snapshot{}, now)
	if err != nil {
		return snapshot{}, fmt.Errorf("%w: %s", ErrRejectedURL, redactErr(err, address))
	}
	return snapshot{
		Name:         name,
		FirstSeen:    now,
		LastFetch:    now,
		LastSuccess:  now,
		Occurrences:  outcome.occurrences,
		WindowEnd:    outcome.windowEnd,
		ETag:         outcome.etag,
		LastModified: outcome.lastModified,
	}, nil
}

// references lists the automation fields that name a source. Without a
// finder no source is referenced.
func (m *Manager) references(ctx context.Context, name string) ([]Reference, error) {
	if m.refs == nil {
		return nil, nil
	}
	return m.refs(ctx, name)
}

// statusOf returns the registry's status of a source the manager just wrote.
func (m *Manager) statusOf(name string) Status {
	for _, s := range m.reg.Statuses() {
		if s.Name == name {
			return s
		}
	}
	return Status{Name: name, Kind: KindICS, Origin: OriginRuntime}
}

// normalizePoll applies the default and the one-minute floor to a poll.
func normalizePoll(d time.Duration) (time.Duration, error) {
	if d == 0 {
		return defaultRuntimePoll, nil
	}
	if d < minPollInterval {
		return 0, fmt.Errorf("%w: must be at least 1m", ErrInvalidPoll)
	}
	return d, nil
}
