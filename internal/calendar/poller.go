package calendar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

const (
	// staleAfter is how long a source may go without a successful fetch before
	// it is stale and its one notice is raised.
	staleAfter = 24 * time.Hour
	// windowBefore and windowAfter bound the expanded window around the poll.
	windowBefore = 24 * time.Hour
	windowAfter  = 14 * 24 * time.Hour
	// minWindowLead is how far past now the cached window must still reach
	// for a poll to stay conditional. Inside that lead the poll fetches in
	// full, so events entering the window are never frozen out by a 304.
	minWindowLead = 24 * time.Hour
	// maxRedirects is how many redirects one fetch may follow.
	maxRedirects = 5
	// defaultFetchTimeout bounds one fetch when PollerOptions.Timeout is zero.
	defaultFetchTimeout = 30 * time.Second
	// minPollInterval is the shortest interval a source may be polled at.
	minPollInterval = time.Minute
	// defaultRuntimePoll is the poll interval of a runtime source saved
	// without one.
	defaultRuntimePoll = 5 * time.Minute
	// staleKind and staleSeverity describe the stale notice.
	staleKind     = "calendar_stale"
	staleSeverity = "warning"
)

// Notice is one system notice the poller raises. It mirrors
// service.SystemNotice field for field; this package cannot import service
// (service imports calendar), so the apiserver adapts between the two.
type Notice struct {
	Kind       string
	Severity   string
	Title      string
	Body       string
	Project    string
	SourceType string
	DedupKey   string
}

// Notifier delivers one notice. A nil Notifier means the notifier is not
// wired yet, and stale notices wait for it.
type Notifier func(ctx context.Context, n Notice) error

// PollerOptions tunes a Poller. The zero value is production behaviour.
type PollerOptions struct {
	// Clock returns the current time. Nil means time.Now.
	Clock func() time.Time
	// Transport carries the HTTPS requests of config sources. Nil means the
	// default transport. Tests inject a TLS test server's transport here.
	// Runtime sources never use it: they have their own guarded client.
	Transport http.RoundTripper
	// Timeout bounds one fetch. Zero means 30 seconds.
	Timeout time.Duration
	// Notify delivers stale notices. It may be nil and wired later with
	// SetNotifier.
	Notify Notifier
	// Logger receives poll outcomes. Nil means slog.Default. Every message is
	// URL-free.
	Logger *slog.Logger
}

// Poller re-reads every ics source on its own schedule and publishes each
// result as a snapshot. A config source's feed URL is resolved from the
// environment or a file on every poll. A runtime source's address is held in
// memory and resolved on every poll too. Neither is stored in a snapshot or
// reported.
//
// Sources can be attached and detached while Run is active. Each source has
// its own poll loop, started under the Run context and stopped by detach.
type Poller struct {
	reg     *Registry
	store   *snapshotStore
	clock   func() time.Time
	logger  *slog.Logger
	client  *http.Client // config sources
	timeout time.Duration

	// smu guards states, loops, runCtx and managed.
	smu     sync.Mutex
	states  map[string]*sourceState
	loops   map[string]*sourceLoop
	runCtx  context.Context
	managed bool

	mu     sync.Mutex // guards notify
	notify Notifier
}

// sourceState is one ics source: its snapshot and, for a runtime source, its
// address and poll interval. mu serializes polls of that source and guards
// every field below it.
type sourceState struct {
	name    string
	mu      sync.Mutex
	snap    snapshot
	removed bool
	client  *http.Client

	// cfg is set for config sources.
	cfg config.CalendarConfig

	// runtime marks a source added through the API. address is its feed
	// address, which is secret, and poll its interval.
	runtime bool
	address string
	poll    time.Duration
}

// sourceLoop is the goroutine that polls one source. done closes when it
// has returned.
type sourceLoop struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewPoller binds a poller to the registry's config ics sources (sources
// holds their configuration) and loads each cached snapshot from
// <dataDir>/calendars, so cached data is served from the first moment. A
// corrupt snapshot is logged and that source starts fresh. A source name that
// is not a safe file name is an error.
func NewPoller(reg *Registry, sources map[string]config.CalendarConfig, dataDir string, opts PollerOptions) (*Poller, error) {
	if reg == nil {
		return nil, errors.New("calendar poller requires a registry")
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	p := &Poller{
		reg:     reg,
		store:   newSnapshotStore(dataDir),
		clock:   clock,
		logger:  logger,
		timeout: timeout,
		states:  make(map[string]*sourceState),
		loops:   make(map[string]*sourceLoop),
		notify:  opts.Notify,
		client: &http.Client{
			Transport:     opts.Transport,
			Timeout:       timeout,
			CheckRedirect: refuseUnsafeRedirect,
		},
	}
	for name, c := range sources {
		if c.Type != KindICS {
			continue
		}
		if _, err := p.store.path(name); err != nil {
			return nil, fmt.Errorf("calendar %q: %w", name, err)
		}
		p.states[name] = &sourceState{name: name, cfg: c, client: p.client}
	}

	names := make([]string, 0, len(p.states))
	for name := range p.states {
		names = append(names, name)
	}
	sort.Strings(names)
	now := clock()
	for _, name := range names {
		st := p.states[name]
		snap, found, err := p.store.load(name)
		if err != nil {
			logger.Warn("calendar snapshot unreadable; starting fresh", "calendar", name, "error", redactErr(err, ""))
			found = false
		}
		if !found {
			snap = snapshot{Name: name}
		}
		snap.Name = name
		if snap.FirstSeen.IsZero() {
			snap.FirstSeen = now
		}
		st.snap = snap
		reg.setSnapshot(snap)
	}
	return p, nil
}

// markManaged records that a manager owns runtime sources. A managed poller's
// Run waits for its context even when no source exists yet, because sources
// may be added later.
func (p *Poller) markManaged() {
	p.smu.Lock()
	defer p.smu.Unlock()
	p.managed = true
}

// SetNotifier wires or replaces the stale-notice sink.
func (p *Poller) SetNotifier(n Notifier) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notify = n
}

func (p *Poller) currentNotifier() Notifier {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.notify
}

// lookup returns the state of a polled source, or nil.
func (p *Poller) lookup(name string) *sourceState {
	p.smu.Lock()
	defer p.smu.Unlock()
	return p.states[name]
}

// Poll runs one fetch cycle for a named ics source and publishes the result.
// Errors are recorded in the source's status and logged; Poll never panics on
// a bad feed. A name that is not polled is a no-op.
func (p *Poller) Poll(ctx context.Context, name string) {
	if st := p.lookup(name); st != nil {
		p.pollState(ctx, st)
	}
}

// pollState runs one fetch cycle for a source. It holds the source's lock, so
// a poll never overlaps another poll or a detach of the same source.
func (p *Poller) pollState(ctx context.Context, st *sourceState) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.removed {
		return
	}
	name := st.name

	now := p.clock()
	snap := st.snap
	snap.LastFetch = now

	feedURL, err := st.feedURL()
	var outcome fetchOutcome
	if err == nil {
		outcome, err = p.fetch(ctx, st.client, name, feedURL, snap, now)
	}
	if err != nil {
		snap.LastError = redactErr(err, feedURL)
		p.logger.Warn("calendar poll failed", "calendar", name, "error", snap.LastError)
	} else {
		snap.LastError = ""
		snap.LastSuccess = now
		if !outcome.notModified {
			snap.Occurrences = outcome.occurrences
			snap.WindowEnd = outcome.windowEnd
			snap.ETag = outcome.etag
			snap.LastModified = outcome.lastModified
		}
		p.logger.Debug("calendar polled", "calendar", name, "events", len(snap.Occurrences))
	}

	p.updateStale(ctx, name, &snap, now)
	p.publish(st, snap)
}

// publish stores a source's snapshot and publishes it to the registry. The
// caller holds the source's lock, or the source is not yet visible to others.
func (p *Poller) publish(st *sourceState, snap snapshot) {
	st.snap = snap
	if err := p.store.save(snap); err != nil {
		p.logger.Warn("calendar snapshot not persisted", "calendar", st.name, "error", redactErr(err, ""))
	}
	p.reg.setSnapshot(snap)
}

// feedURL returns the address a poll fetches now. The caller holds the lock.
func (st *sourceState) feedURL() (string, error) {
	if st.runtime {
		if st.address == "" {
			return "", fmt.Errorf("calendar %q: no address", st.name)
		}
		return st.address, nil
	}
	return resolveFeedURL(st.name, st.cfg)
}

// interval is how often the source is re-read: its poll, never below one
// minute. Config sources use their configured poll.
func (st *sourceState) interval() time.Duration {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.runtime {
		return PollInterval(st.cfg)
	}
	d := st.poll
	if d == 0 {
		d = defaultRuntimePoll
	}
	if d < minPollInterval {
		d = minPollInterval
	}
	return d
}

// Run polls every ics source on its own interval until ctx is cancelled, then
// returns after all source loops have stopped. With no ics sources and no
// manager it returns at once. A managed poller waits for ctx even with no
// sources, because sources may be attached later.
func (p *Poller) Run(ctx context.Context) {
	p.smu.Lock()
	if len(p.states) == 0 && !p.managed {
		p.smu.Unlock()
		return
	}
	p.runCtx = ctx
	for name, st := range p.states {
		p.startLoopLocked(name, st, 0)
	}
	p.smu.Unlock()

	<-ctx.Done()

	p.smu.Lock()
	p.runCtx = nil
	loops := p.loops
	p.loops = make(map[string]*sourceLoop)
	for _, l := range loops {
		l.cancel()
	}
	p.smu.Unlock()
	for _, l := range loops {
		<-l.done
	}
}

// startLoopLocked starts the poll loop of one source under the Run context.
// The caller holds smu and has checked that Run is active.
func (p *Poller) startLoopLocked(name string, st *sourceState, first time.Duration) {
	lctx, cancel := context.WithCancel(p.runCtx)
	l := &sourceLoop{cancel: cancel, done: make(chan struct{})}
	p.loops[name] = l
	go func() {
		defer close(l.done)
		p.runSource(lctx, st, first)
	}()
}

// runSource polls one source after first, then every interval plus a small
// per-source offset, so sources do not all fire together.
func (p *Poller) runSource(ctx context.Context, st *sourceState, first time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		p.pollState(ctx, st)
		interval := st.interval()
		timer.Reset(interval + jitterFor(st.name, interval))
	}
}

// attachRuntime starts polling a runtime source. snap is its first snapshot:
// it is stored and published before polling starts. firstPoll is the delay
// before the first loop poll when Run is already active. The registry must
// already know the name. client is the source's own guarded client.
func (p *Poller) attachRuntime(name, address string, poll time.Duration, client *http.Client, snap snapshot, firstPoll time.Duration) error {
	if p.lookup(name) != nil {
		return fmt.Errorf("calendar %q is already polled", name)
	}
	if _, err := p.store.path(name); err != nil {
		return fmt.Errorf("calendar %q: %w", name, err)
	}
	snap.Name = name
	if snap.FirstSeen.IsZero() {
		snap.FirstSeen = p.clock()
	}
	st := &sourceState{
		name:    name,
		client:  client,
		runtime: true,
		address: address,
		poll:    poll,
	}
	p.publish(st, snap)

	p.smu.Lock()
	defer p.smu.Unlock()
	if _, exists := p.states[name]; exists {
		return fmt.Errorf("calendar %q is already polled", name)
	}
	p.states[name] = st
	if p.runCtx != nil {
		p.startLoopLocked(name, st, firstPoll)
	}
	return nil
}

// detach stops polling a source and forgets it. It cancels the source's poll
// loop and waits for the loop to end, so no fetch or snapshot write for the
// source happens after detach returns. Detaching an unknown name is a no-op.
func (p *Poller) detach(name string) {
	p.smu.Lock()
	st, ok := p.states[name]
	if !ok {
		p.smu.Unlock()
		return
	}
	delete(p.states, name)
	l := p.loops[name]
	delete(p.loops, name)
	p.smu.Unlock()

	if l != nil {
		l.cancel()
		<-l.done
	}
	st.mu.Lock()
	st.removed = true
	st.mu.Unlock()
	st.client.CloseIdleConnections()
}

// setAddress replaces a runtime source's address. The next poll uses it. snap
// is the snapshot fetched from the new address during validation, and it
// replaces the cached one so no data from the old feed remains.
func (p *Poller) setAddress(name, address string, snap snapshot) error {
	st := p.lookup(name)
	if st == nil {
		return errors.New("calendar is not polled")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.removed || !st.runtime {
		return errors.New("calendar is not a runtime source")
	}
	st.address = address
	snap.Name = name
	if snap.FirstSeen.IsZero() {
		snap.FirstSeen = st.snap.FirstSeen
	}
	p.publish(st, snap)
	return nil
}

// setRuntimePoll changes a runtime source's interval. The next cycle uses it.
func (p *Poller) setRuntimePoll(name string, poll time.Duration) error {
	st := p.lookup(name)
	if st == nil {
		return errors.New("calendar is not polled")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.removed || !st.runtime {
		return errors.New("calendar is not a runtime source")
	}
	st.poll = poll
	return nil
}

// fetchOutcome is the result of one successful HTTP exchange.
type fetchOutcome struct {
	notModified  bool
	occurrences  []Occurrence
	windowEnd    time.Time
	etag         string
	lastModified string
}

// fetch performs one conditional GET with client and expands the body. Every
// error it returns may contain the feed URL, so callers must pass it through
// redactErr.
func (p *Poller) fetch(ctx context.Context, client *http.Client, name, feedURL string, snap snapshot, now time.Time) (fetchOutcome, error) {
	u, err := url.Parse(feedURL)
	if err != nil || u.Host == "" {
		return fetchOutcome{}, errors.New("url is not a valid absolute URL")
	}
	if u.Scheme != "https" {
		return fetchOutcome{}, errors.New("url must use https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return fetchOutcome{}, errors.New("could not build the feed request")
	}
	req.Header.Set("Accept", "text/calendar, */*;q=0.1")
	// A 304 keeps the cached occurrences, which are only correct while the
	// window they were expanded for still reaches ahead of now. Past that
	// point the fetch is unconditional so the window moves forward.
	conditional := !snap.WindowEnd.IsZero() && now.Add(minWindowLead).Before(snap.WindowEnd)
	if conditional && snap.ETag != "" {
		req.Header.Set("If-None-Match", snap.ETag)
	}
	if conditional && snap.LastModified != "" {
		req.Header.Set("If-Modified-Since", snap.LastModified)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fetchOutcome{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		if !conditional || snap.LastSuccess.IsZero() {
			return fetchOutcome{}, errors.New("HTTP 304 without a cached feed to keep")
		}
		return fetchOutcome{notModified: true}, nil
	case resp.StatusCode != http.StatusOK:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return fetchOutcome{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFeedBytes+1))
	if err != nil {
		return fetchOutcome{}, fmt.Errorf("read feed: %w", err)
	}
	if len(body) > MaxFeedBytes {
		return fetchOutcome{}, fmt.Errorf("feed exceeds the %d MiB size limit", MaxFeedBytes>>20)
	}
	feed, err := Parse(bytes.NewReader(body))
	if err != nil {
		return fetchOutcome{}, fmt.Errorf("parse feed: %w", err)
	}
	occ, err := feed.ExpandWithOptions(now.Add(-windowBefore), now.Add(windowAfter), ExpandOptions{DefaultLocation: time.UTC})
	if err != nil {
		return fetchOutcome{}, fmt.Errorf("expand feed: %w", err)
	}
	for i := range occ {
		occ[i].Calendar = name
	}
	return fetchOutcome{
		occurrences:  occ,
		windowEnd:    now.Add(windowAfter),
		etag:         resp.Header.Get("ETag"),
		lastModified: resp.Header.Get("Last-Modified"),
	}, nil
}

// updateStale recomputes whether the source is stale and raises its notice
// once per episode. An episode starts 24 hours after the last success (or
// after first sight, if there has never been one). The notice is recorded as
// delivered only after the notifier accepts it, so an unwired or failing
// notifier is retried on the next poll.
func (p *Poller) updateStale(ctx context.Context, name string, snap *snapshot, now time.Time) {
	ref := snap.LastSuccess
	if ref.IsZero() {
		ref = snap.FirstSeen
	}
	episode := ref.Add(staleAfter)
	if now.Before(episode) {
		snap.Stale = false
		return
	}
	snap.Stale = true
	if snap.NotifiedEpisode.Equal(episode) {
		return
	}
	notify := p.currentNotifier()
	if notify == nil {
		return
	}
	notice := Notice{
		Kind:       staleKind,
		Severity:   staleSeverity,
		Title:      fmt.Sprintf("Calendar %s is stale", name),
		Body:       fmt.Sprintf("No successful refresh since %s. Automations that read this calendar may use outdated events.", ref.UTC().Format(time.RFC3339)),
		SourceType: "calendar",
		DedupKey:   "calendar-stale:" + name + ":" + episode.UTC().Format(time.RFC3339),
	}
	if err := notify(ctx, notice); err != nil {
		p.logger.Warn("calendar stale notice not delivered; will retry", "calendar", name, "error", redactErr(err, ""))
		return
	}
	snap.NotifiedEpisode = episode
}

// jitterFor is a fixed offset in [0, interval/10) derived from the name.
func jitterFor(name string, interval time.Duration) time.Duration {
	span := interval / 10
	if span <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return time.Duration(h.Sum32()) % span
}

// PollInterval is how often a config ics source is re-read: its configured
// poll, defaulting to five minutes and never below one minute.
func PollInterval(c config.CalendarConfig) time.Duration {
	d := c.PollInterval()
	if d < minPollInterval {
		d = minPollInterval
	}
	return d
}

// resolveFeedURL reads the feed URL for a config source now, so rotating the
// environment variable or url_file takes effect on the next poll. Its errors
// name the calendar and the setting, never the URL or the file contents.
func resolveFeedURL(name string, c config.CalendarConfig) (string, error) {
	switch {
	case c.URLEnv != "":
		v := strings.TrimSpace(os.Getenv(c.URLEnv))
		if v == "" {
			return "", fmt.Errorf("calendar %q: url_env %s is unset or empty", name, c.URLEnv)
		}
		return v, nil
	case c.URLFile != "":
		b, err := os.ReadFile(c.URLFile)
		if err != nil {
			return "", fmt.Errorf("calendar %q: url_file is unreadable", name)
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			return "", fmt.Errorf("calendar %q: url_file is empty", name)
		}
		return v, nil
	default:
		return "", fmt.Errorf("calendar %q: no url source configured", name)
	}
}

// refuseUnsafeRedirect follows at most maxRedirects redirects and only to
// https. It never sends credentials: the poller sets no Authorization and has
// no cookie jar.
func refuseUnsafeRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != "https" {
		return errors.New("redirect to a non-https url refused")
	}
	return nil
}

// redactErr returns err's message with the URL-bearing parts of raw removed.
// It unwraps a *url.Error first, because that wrapper carries the full URL.
func redactErr(err error, raw string) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	msg := err.Error()
	for _, secret := range secretParts(raw) {
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	return msg
}

// secretParts lists the strings of a feed URL that must never be echoed: the
// whole URL, and its path and query, which carry the token.
func secretParts(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := []string{raw}
	u, err := url.Parse(raw)
	if err != nil {
		return parts
	}
	for _, p := range []string{u.Path, u.RawPath, u.RawQuery, u.Fragment} {
		if len(p) >= 4 {
			parts = append(parts, p)
		}
	}
	return parts
}
