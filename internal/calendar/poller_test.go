package calendar

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

// ---- helpers --------------------------------------------------------------

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// syncBuffer collects log output from concurrent goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// noticeLog records every notice a poller raises.
type noticeLog struct {
	mu    sync.Mutex
	items []Notice
}

func (n *noticeLog) record(_ context.Context, notice Notice) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.items = append(n.items, notice)
	return nil
}

func (n *noticeLog) all() []Notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Notice(nil), n.items...)
}

// icsEvent is one VEVENT the test feed carries.
type icsEvent struct {
	uid, title string
	start      time.Time
}

// icsFeed renders a minimal iCalendar feed with CRLF line endings.
func icsFeed(events ...icsEvent) []byte {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n")
	for _, e := range events {
		start := e.start.UTC()
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString("UID:" + e.uid + "\r\n")
		b.WriteString("DTSTAMP:20261001T000000Z\r\n")
		b.WriteString("DTSTART:" + start.Format("20060102T150405Z") + "\r\n")
		b.WriteString("DTEND:" + start.Add(time.Hour).Format("20060102T150405Z") + "\r\n")
		b.WriteString("SUMMARY:" + e.title + "\r\n")
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return []byte(b.String())
}

// feedServer is an httptest TLS server that counts requests and records their
// paths. Behaviour comes from handler, which tests may change between polls.
type feedServer struct {
	srv   *httptest.Server
	hits  atomic.Int32
	mu    sync.Mutex
	paths []string
	heads []http.Header
	mode  atomic.Value // func(http.ResponseWriter, *http.Request)
}

func newFeedServer(t *testing.T, handler http.HandlerFunc) *feedServer {
	t.Helper()
	f := &feedServer{}
	f.mode.Store(handler)
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.heads = append(f.heads, r.Header.Clone())
		f.mu.Unlock()
		f.mode.Load().(http.HandlerFunc)(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *feedServer) setMode(h http.HandlerFunc) { f.mode.Store(h) }

func (f *feedServer) transport() http.RoundTripper { return f.srv.Client().Transport }

func (f *feedServer) lastPath() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.paths) == 0 {
		return ""
	}
	return f.paths[len(f.paths)-1]
}

func (f *feedServer) lastHeader() http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.heads) == 0 {
		return http.Header{}
	}
	return f.heads[len(f.heads)-1]
}

func serveFeed(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write(body)
	}
}

func statusCode(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }
}

// pollHarness wires a poller over one ics source named "team".
type pollHarness struct {
	t       *testing.T
	reg     *Registry
	poller  *Poller
	clock   *fakeClock
	dataDir string
	logs    *syncBuffer
	notices *noticeLog
}

func newPollHarness(t *testing.T, dataDir string, clock *fakeClock, transport http.RoundTripper, timeout time.Duration, withNotifier bool) *pollHarness {
	t.Helper()
	sources := map[string]config.CalendarConfig{"team": {Type: KindICS, URLEnv: "CAL_TEAM_URL"}}
	reg, err := NewRegistry(sources)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	h := &pollHarness{t: t, reg: reg, clock: clock, dataDir: dataDir, logs: &syncBuffer{}, notices: &noticeLog{}}
	h.poller = h.newPoller(sources, transport, timeout, withNotifier)
	return h
}

func (h *pollHarness) newPoller(sources map[string]config.CalendarConfig, transport http.RoundTripper, timeout time.Duration, withNotifier bool) *Poller {
	h.t.Helper()
	opts := PollerOptions{
		Clock:     h.clock.Now,
		Transport: transport,
		Timeout:   timeout,
		Logger:    slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if withNotifier {
		opts.Notify = h.notices.record
	}
	p, err := NewPoller(h.reg, sources, h.dataDir, opts)
	if err != nil {
		h.t.Fatalf("NewPoller: %v", err)
	}
	return p
}

func (h *pollHarness) status() Status {
	h.t.Helper()
	for _, s := range h.reg.Statuses() {
		if s.Name == "team" {
			return s
		}
	}
	h.t.Fatal("team missing from registry statuses")
	return Status{}
}

func (h *pollHarness) occurrences() []Occurrence {
	h.t.Helper()
	occ, _, ok := h.reg.Occurrences("team")
	if !ok {
		h.t.Fatal("team unknown to registry")
	}
	return occ
}

// ---- tests ----------------------------------------------------------------

func TestPollFetchesExpandsWindowAndPublishes(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	now := clock.Now()
	body := icsFeed(
		icsEvent{uid: "in", title: "in window", start: now.Add(2 * time.Hour)},
		icsEvent{uid: "far", title: "too far", start: now.Add(60 * 24 * time.Hour)},
		icsEvent{uid: "old", title: "too old", start: now.Add(-3 * 24 * time.Hour)},
	)
	srv := newFeedServer(t, serveFeed(body))
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/SECRET-TOKEN-1/cal.ics")

	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 0, true)
	h.poller.Poll(context.Background(), "team")

	if got := srv.lastPath(); got != "/feed/SECRET-TOKEN-1/cal.ics" {
		t.Fatalf("server path: got %q; URL from url_env was not fetched", got)
	}
	occ := h.occurrences()
	if len(occ) != 1 {
		t.Fatalf("occurrences in [now-1d, now+14d]: got %d want 1: %+v", len(occ), occ)
	}
	if occ[0].Title != "in window" || occ[0].Calendar != "team" {
		t.Fatalf("occurrence: %+v", occ[0])
	}
	s := h.status()
	if s.Kind != KindICS || s.EventCount != 1 || s.LastError != "" || s.Stale {
		t.Fatalf("status after success: %+v", s)
	}
	if !s.LastSuccess.Equal(now) || !s.LastFetch.Equal(now) {
		t.Fatalf("status times: %+v want %v", s, now)
	}
}

func TestPollConditionalGETReusesSnapshotOn304(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	now := clock.Now()
	body := icsFeed(icsEvent{uid: "in", title: "kept", start: now.Add(time.Hour)})
	srv := newFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Fri, 09 Oct 2026 10:00:00 GMT")
		_, _ = w.Write(body)
	})
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 0, true)

	h.poller.Poll(context.Background(), "team")
	clock.Advance(10 * time.Minute)
	h.poller.Poll(context.Background(), "team")

	hdr := srv.lastHeader()
	if hdr.Get("If-None-Match") != `"v1"` {
		t.Fatalf("second poll did not send If-None-Match: %v", hdr)
	}
	if hdr.Get("Authorization") != "" || hdr.Get("Cookie") != "" {
		t.Fatalf("poll sent credentials or cookies: %v", hdr)
	}
	s := h.status()
	if s.EventCount != 1 || s.LastError != "" {
		t.Fatalf("304 must keep the snapshot: %+v", s)
	}
	if !s.LastSuccess.Equal(clock.Now()) {
		t.Fatalf("304 must count as success: LastSuccess=%v want %v", s.LastSuccess, clock.Now())
	}
	if occ := h.occurrences(); len(occ) != 1 || occ[0].Title != "kept" {
		t.Fatalf("occurrences after 304: %+v", occ)
	}
}

func TestPollServerErrorKeepsLastGoodData(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	now := clock.Now()
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "in", title: "good", start: now.Add(time.Hour)})))
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 0, true)

	h.poller.Poll(context.Background(), "team")
	firstSuccess := h.status().LastSuccess
	srv.setMode(statusCode(http.StatusServiceUnavailable))
	clock.Advance(5 * time.Minute)
	h.poller.Poll(context.Background(), "team")

	s := h.status()
	if !strings.Contains(s.LastError, "503") {
		t.Fatalf("LastError should name the HTTP status: %q", s.LastError)
	}
	if !s.LastSuccess.Equal(firstSuccess) {
		t.Fatalf("failed poll moved LastSuccess: %v want %v", s.LastSuccess, firstSuccess)
	}
	if s.EventCount != 1 {
		t.Fatalf("failed poll dropped the last good window: %+v", s)
	}
}

func TestPollTimeoutIsAnErrorNotAHang(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 50*time.Millisecond, true)

	h.poller.Poll(context.Background(), "team")

	s := h.status()
	if s.LastError == "" {
		t.Fatal("timeout produced no LastError")
	}
	if strings.Contains(s.LastError, srv.srv.URL) {
		t.Fatalf("timeout error echoed the URL: %q", s.LastError)
	}
}

func TestPollRejectsOversizeBody(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	huge := bytes.Repeat([]byte("X"), (10<<20)+1)
	srv := newFeedServer(t, serveFeed(huge))
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 0, true)

	h.poller.Poll(context.Background(), "team")

	s := h.status()
	if s.LastError == "" || !strings.Contains(s.LastError, "size") {
		t.Fatalf("oversize body: LastError=%q", s.LastError)
	}
	if s.EventCount != 0 || !s.LastSuccess.IsZero() {
		t.Fatalf("oversize body was accepted: %+v", s)
	}
}

func TestPollRedirectLoopIsCapped(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	var self string
	srv := newFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, self, http.StatusFound)
	})
	self = srv.srv.URL + "/feed/tok/loop"
	t.Setenv("CAL_TEAM_URL", self)
	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 0, true)

	h.poller.Poll(context.Background(), "team")

	if got := srv.hits.Load(); got > 6 {
		t.Fatalf("redirect loop: %d requests, want at most 6 (1 + 5 redirects)", got)
	}
	if s := h.status(); s.LastError == "" {
		t.Fatal("redirect loop produced no LastError")
	}
}

func TestPollRejectsPlainHTTP(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	t.Setenv("CAL_TEAM_URL", "http://127.0.0.1:1/feed/PLAIN-SECRET/cal.ics")
	h := newPollHarness(t, t.TempDir(), clock, nil, 0, true)

	h.poller.Poll(context.Background(), "team")

	s := h.status()
	if !strings.Contains(s.LastError, "https") {
		t.Fatalf("plain http not refused with an https message: %q", s.LastError)
	}
	assertNoSecret(t, "PLAIN-SECRET", s.LastError, h.logs.String())
}

func TestPollRefusesRedirectToPlainHTTP(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/feed/DOWNGRADE-SECRET/cal.ics", http.StatusFound)
	})
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 0, true)

	h.poller.Poll(context.Background(), "team")

	s := h.status()
	if s.LastError == "" {
		t.Fatal("downgrade redirect was followed")
	}
	assertNoSecret(t, "DOWNGRADE-SECRET", s.LastError, h.logs.String())
}

func TestPollURLFileRotationTakesEffectOnNextPoll(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, serveFeed(icsFeed()))
	urlFile := filepath.Join(t.TempDir(), "team.url")
	if err := os.WriteFile(urlFile, []byte(srv.srv.URL+"/feed/OLD-TOKEN/cal.ics\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := map[string]config.CalendarConfig{"team": {Type: KindICS, URLFile: urlFile}}
	reg, err := NewRegistry(sources)
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	p, err := NewPoller(reg, sources, t.TempDir(), PollerOptions{
		Clock:     clock.Now,
		Transport: srv.transport(),
		Logger:    slog.New(slog.NewTextHandler(logs, nil)),
	})
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}

	p.Poll(context.Background(), "team")
	if got := srv.lastPath(); got != "/feed/OLD-TOKEN/cal.ics" {
		t.Fatalf("first poll used %q", got)
	}

	if err := os.WriteFile(urlFile, []byte(srv.srv.URL+"/feed/NEW-TOKEN/cal.ics\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	clock.Advance(5 * time.Minute)
	p.Poll(context.Background(), "team")
	if got := srv.lastPath(); got != "/feed/NEW-TOKEN/cal.ics" {
		t.Fatalf("rotated url_file not re-read on the next poll: got %q", got)
	}
}

// TestSecretTokenNeverLeaksIntoErrorsLogsOrStatus drives every failure path
// with a token in the feed URL and checks the token is absent from every
// LastError, every log line, every status field, and the persisted snapshot.
func TestSecretTokenNeverLeaksIntoErrorsLogsOrStatus(t *testing.T) {
	const token = "TOKEN-f81c2e9a7d"
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}

	scenarios := map[string]func(t *testing.T) (string, http.RoundTripper){
		"connection refused": func(t *testing.T) (string, http.RoundTripper) {
			srv := newFeedServer(t, serveFeed(icsFeed()))
			base := srv.srv.URL
			tr := srv.transport()
			srv.srv.Close()
			return base + "/feed/" + token + "/cal.ics", tr
		},
		"server 500": func(t *testing.T) (string, http.RoundTripper) {
			srv := newFeedServer(t, statusCode(http.StatusInternalServerError))
			return srv.srv.URL + "/feed/" + token + "/cal.ics", srv.transport()
		},
		"redirect to plain http": func(t *testing.T) (string, http.RoundTripper) {
			srv := newFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://127.0.0.1:1/feed/"+token+"/x", http.StatusFound)
			})
			return srv.srv.URL + "/feed/" + token + "/cal.ics", srv.transport()
		},
		"plain http source": func(t *testing.T) (string, http.RoundTripper) {
			return "http://127.0.0.1:1/feed/" + token + "/cal.ics", nil
		},
		"malformed body": func(t *testing.T) (string, http.RoundTripper) {
			srv := newFeedServer(t, serveFeed([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n")))
			return srv.srv.URL + "/feed/" + token + "/cal.ics", srv.transport()
		},
	}

	for name, setup := range scenarios {
		t.Run(name, func(t *testing.T) {
			raw, tr := setup(t)
			t.Setenv("CAL_TEAM_URL", raw)
			dataDir := t.TempDir()
			h := newPollHarness(t, dataDir, clock, tr, 0, true)

			h.poller.Poll(context.Background(), "team")

			s := h.status()
			if s.LastError == "" {
				t.Fatal("scenario produced no error to inspect")
			}
			onDisk, err := os.ReadFile(filepath.Join(dataDir, "calendars", "team.json"))
			if err != nil {
				t.Fatalf("snapshot not persisted: %v", err)
			}
			assertNoSecret(t, token, s.LastError, fmt.Sprintf("%+v", s), h.logs.String(), string(onDisk))
		})
	}
}

func TestRestartLoadsCachedSnapshotImmediately(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	now := clock.Now()
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "in", title: "cached", start: now.Add(time.Hour)})))
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	dataDir := t.TempDir()
	h := newPollHarness(t, dataDir, clock, srv.transport(), 0, true)
	h.poller.Poll(context.Background(), "team")
	want := h.status()

	// Restart: fresh registry and poller over the same data dir, server gone.
	srv.srv.Close()
	restarted := newPollHarness(t, dataDir, clock, nil, 0, true)

	occ := restarted.occurrences()
	if len(occ) != 1 || occ[0].Title != "cached" {
		t.Fatalf("restart did not serve the cached occurrences: %+v", occ)
	}
	got := restarted.status()
	if got.EventCount != 1 || !got.LastSuccess.Equal(want.LastSuccess) {
		t.Fatalf("restart status: %+v want success at %v", got, want.LastSuccess)
	}
}

func TestStaleRaisesOneNoticePerEpisodeAndClearsOnSuccess(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	now := clock.Now()
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "in", title: "x", start: now.Add(time.Hour)})))
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	dataDir := t.TempDir()
	h := newPollHarness(t, dataDir, clock, srv.transport(), 0, true)
	ctx := context.Background()

	h.poller.Poll(ctx, "team") // success at t0
	t0 := clock.Now()
	srv.setMode(statusCode(http.StatusServiceUnavailable))

	clock.Advance(23 * time.Hour)
	h.poller.Poll(ctx, "team")
	if n := len(h.notices.all()); n != 0 || h.status().Stale {
		t.Fatalf("notice before 24h: count=%d status=%+v", n, h.status())
	}

	clock.Advance(2 * time.Hour) // 25h since last success
	h.poller.Poll(ctx, "team")
	h.poller.Poll(ctx, "team")
	clock.Advance(time.Hour)
	h.poller.Poll(ctx, "team")
	items := h.notices.all()
	if len(items) != 1 {
		t.Fatalf("stale episode raised %d notices, want exactly 1", len(items))
	}
	wantKey := "calendar-stale:team:" + t0.Add(24*time.Hour).UTC().Format(time.RFC3339)
	n := items[0]
	if n.Kind != "calendar_stale" || n.Severity != "warning" || n.SourceType != "calendar" || n.DedupKey != wantKey {
		t.Fatalf("stale notice: %+v want key %q", n, wantKey)
	}
	if !h.status().Stale {
		t.Fatal("status not stale after 24h without success")
	}

	// A restart in the middle of the episode must not notify again.
	restarted := newPollHarness(t, dataDir, clock, srv.transport(), 0, true)
	restarted.poller.Poll(ctx, "team")
	restartedItems := restarted.notices.all()
	if len(restartedItems) != 0 {
		t.Fatalf("restart re-raised the stale notice: %+v", restartedItems)
	}
	// The restarted harness has its own notice log; the shared one is h.notices.
	if len(h.notices.all()) != 1 {
		t.Fatalf("notice count changed after restart: %d", len(h.notices.all()))
	}

	// Success clears the episode.
	srv.setMode(serveFeed(icsFeed(icsEvent{uid: "in", title: "x", start: clock.Now().Add(time.Hour)})))
	clock.Advance(time.Minute)
	restarted.poller.Poll(ctx, "team")
	if restarted.status().Stale {
		t.Fatal("success did not clear the stale episode")
	}
	t1 := clock.Now()

	// A new episode after the success raises exactly one more notice.
	srv.setMode(statusCode(http.StatusServiceUnavailable))
	clock.Advance(25 * time.Hour)
	restarted.poller.Poll(ctx, "team")
	restarted.poller.Poll(ctx, "team")
	second := restarted.notices.all()
	if len(second) != 1 {
		t.Fatalf("second episode raised %d notices, want 1", len(second))
	}
	wantSecond := "calendar-stale:team:" + t1.Add(24*time.Hour).UTC().Format(time.RFC3339)
	if second[0].DedupKey != wantSecond {
		t.Fatalf("second episode key %q want %q", second[0].DedupKey, wantSecond)
	}
}

func TestStaleNoticeWaitsForNotifierAndRetriesOnError(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, statusCode(http.StatusServiceUnavailable))
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 0, false) // no notifier wired yet
	ctx := context.Background()

	clock.Advance(25 * time.Hour) // never succeeded: stale from FirstSeen+24h
	h.poller.Poll(ctx, "team")
	if len(h.notices.all()) != 0 {
		t.Fatal("notice raised before a notifier was wired")
	}
	if !h.status().Stale {
		t.Fatal("never-fetched source not stale after 24h from first seen")
	}

	var attempts atomic.Int32
	h.poller.SetNotifier(func(ctx context.Context, n Notice) error {
		if attempts.Add(1) == 1 {
			return fmt.Errorf("attention store unavailable")
		}
		return h.notices.record(ctx, n)
	})
	h.poller.Poll(ctx, "team") // first delivery fails
	h.poller.Poll(ctx, "team") // retried and delivered
	h.poller.Poll(ctx, "team") // already delivered for this episode
	if got := attempts.Load(); got != 2 {
		t.Fatalf("notifier attempts: got %d want 2 (one failure, one delivery)", got)
	}
	if len(h.notices.all()) != 1 {
		t.Fatalf("delivered notices: %d want 1", len(h.notices.all()))
	}
}

func TestPollIntervalHasDefaultAndOneMinuteFloor(t *testing.T) {
	cases := map[string]time.Duration{
		"":    5 * time.Minute,
		"30s": time.Minute,
		"2m":  2 * time.Minute,
		"10m": 10 * time.Minute,
	}
	for poll, want := range cases {
		if got := PollInterval(config.CalendarConfig{Type: KindICS, Poll: poll}); got != want {
			t.Errorf("PollInterval(%q) = %v, want %v", poll, got, want)
		}
	}
}

func TestRunPollsEverySourceAndStopsOnCancel(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	srv := newFeedServer(t, serveFeed(icsFeed(icsEvent{uid: "in", title: "run", start: clock.Now().Add(time.Hour)})))
	t.Setenv("CAL_TEAM_URL", srv.srv.URL+"/feed/tok/cal.ics")
	h := newPollHarness(t, t.TempDir(), clock, srv.transport(), 0, true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.poller.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(20 * time.Second)
	for h.status().LastSuccess.IsZero() {
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("Run never polled the source")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRunWithoutIcsSourcesReturnsImmediately(t *testing.T) {
	reg, err := NewRegistry(map[string]config.CalendarConfig{
		"xnys": {Type: KindBuiltin, Market: "XNYS"},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPoller(reg, map[string]config.CalendarConfig{"xnys": {Type: KindBuiltin, Market: "XNYS"}}, t.TempDir(), PollerOptions{})
	if err != nil {
		t.Fatalf("NewPoller with builtin only: %v", err)
	}
	done := make(chan struct{})
	go func() { p.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run with no ics sources did not return")
	}
}

func TestNewPollerRefusesUnsafeSourceName(t *testing.T) {
	sources := map[string]config.CalendarConfig{"../escape": {Type: KindICS, URLEnv: "X"}}
	reg, err := NewRegistry(map[string]config.CalendarConfig{"ok": {Type: KindICS, URLEnv: "X"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPoller(reg, sources, t.TempDir(), PollerOptions{}); err == nil {
		t.Fatal("NewPoller accepted a source name that is not a safe file name")
	}
}

// assertNoSecret fails when secret appears in any of the given texts.
func assertNoSecret(t *testing.T, secret string, texts ...string) {
	t.Helper()
	for i, text := range texts {
		if strings.Contains(text, secret) {
			t.Fatalf("secret leaked into text %d: %q", i, text)
		}
	}
}
