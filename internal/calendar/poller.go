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
	// Transport carries the HTTPS requests. Nil means the default transport.
	// Tests inject a TLS test server's transport here.
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

// Poller re-reads every ics source of a registry on its own schedule and
// publishes each result as a snapshot. The feed URL is resolved from the
// environment or a file on every poll and is never stored or reported.
type Poller struct {
	reg     *Registry
	store   *snapshotStore
	sources map[string]config.CalendarConfig
	states  map[string]*sourceState
	clock   func() time.Time
	client  *http.Client
	logger  *slog.Logger

	mu     sync.Mutex // guards notify
	notify Notifier
}

// sourceState is one ics source's snapshot. mu serializes polls of that source.
type sourceState struct {
	mu   sync.Mutex
	snap snapshot
}

// NewPoller binds a poller to the registry's ics sources (sources holds their
// configuration) and loads each cached snapshot from <dataDir>/calendars, so
// cached data is served from the first moment. A corrupt snapshot is logged
// and that source starts fresh. A source name that is not a safe file name is
// an error.
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
		sources: make(map[string]config.CalendarConfig),
		states:  make(map[string]*sourceState),
		clock:   clock,
		logger:  logger,
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
		p.sources[name] = c
	}

	names := make([]string, 0, len(p.sources))
	for name := range p.sources {
		names = append(names, name)
	}
	sort.Strings(names)
	now := clock()
	for _, name := range names {
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
		p.states[name] = &sourceState{snap: snap}
		reg.setSnapshot(snap)
	}
	return p, nil
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

// Poll runs one fetch cycle for a named ics source and publishes the result.
// Errors are recorded in the source's status and logged; Poll never panics on
// a bad feed.
func (p *Poller) Poll(ctx context.Context, name string) {
	st, ok := p.states[name]
	if !ok {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	now := p.clock()
	snap := st.snap
	snap.LastFetch = now

	feedURL, err := resolveFeedURL(name, p.sources[name])
	var outcome fetchOutcome
	if err == nil {
		outcome, err = p.fetch(ctx, name, feedURL, snap, now)
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
	st.snap = snap
	if err := p.store.save(snap); err != nil {
		p.logger.Warn("calendar snapshot not persisted", "calendar", name, "error", redactErr(err, feedURL))
	}
	p.reg.setSnapshot(snap)
}

// Run polls every ics source on its own interval until ctx is cancelled, then
// returns after all source loops have stopped. With no ics sources it returns
// at once.
func (p *Poller) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for name := range p.states {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			p.runSource(ctx, name)
		}(name)
	}
	wg.Wait()
}

// runSource polls one source immediately, then every interval plus a small
// per-source offset, so sources do not all fire together.
func (p *Poller) runSource(ctx context.Context, name string) {
	interval := PollInterval(p.sources[name])
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		p.Poll(ctx, name)
		timer.Reset(interval + jitterFor(name, interval))
	}
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

// PollInterval is how often an ics source is re-read: its configured poll,
// defaulting to five minutes and never below one minute.
func PollInterval(c config.CalendarConfig) time.Duration {
	d := c.PollInterval()
	if d < minPollInterval {
		d = minPollInterval
	}
	return d
}

// fetchOutcome is the result of one successful HTTP exchange.
type fetchOutcome struct {
	notModified  bool
	occurrences  []Occurrence
	windowEnd    time.Time
	etag         string
	lastModified string
}

// fetch performs one conditional GET and expands the body. Every error it
// returns may contain the feed URL, so callers must pass it through redactErr.
func (p *Poller) fetch(ctx context.Context, name, feedURL string, snap snapshot, now time.Time) (fetchOutcome, error) {
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

	resp, err := p.client.Do(req)
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

// resolveFeedURL reads the feed URL for a source now, so rotating the
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
