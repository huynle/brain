package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
)

// The feed address carries a secret token. No response body and no log line
// may contain it.
const runtimeSecretToken = "s3cret-token-e2e"

// runtimeLogBuffer collects slog output from the poller's goroutines.
type runtimeLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *runtimeLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *runtimeLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs routes the default logger into a buffer for the test's life.
func captureLogs(t *testing.T) *runtimeLogBuffer {
	t.Helper()
	out := &runtimeLogBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return out
}

// icsFeedNow renders a feed with one event an hour from now.
func icsFeedNow(uid, title string) []byte {
	start := time.Now().UTC().Add(time.Hour)
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20261001T000000Z\r\n" +
		"DTSTART:" + start.Format("20060102T150405Z") + "\r\n" +
		"DTEND:" + start.Add(time.Hour).Format("20060102T150405Z") + "\r\n" +
		"SUMMARY:" + title + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
}

// runtimeFeed is a loopback TLS feed that stands in for a Google iCal address.
// calendarTestSeam routes runtime sources to it for the test's life.
func runtimeFeed(t *testing.T) (feedURL string) {
	t.Helper()
	feed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write(icsFeedNow("standup", "Standup"))
	}))
	t.Cleanup(feed.Close)
	tlsCfg := feed.Client().Transport.(*http.Transport).TLSClientConfig
	prev := calendarTestSeam
	calendarTestSeam = &calendar.TestSeam{
		Allowed: func(a netip.Addr) bool { return a.IsLoopback() },
		TLS:     tlsCfg,
	}
	t.Cleanup(func() { calendarTestSeam = prev })
	return feed.URL + "/private/" + runtimeSecretToken + "/basic.ics"
}

// bootCalendarServer builds the single-mode HTTP handler over brainDir and
// returns it with a cleanup that stops the workers.
func bootCalendarServer(t *testing.T, brainDir string, opts ServerOptions) (http.Handler, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	opts.Host = "localhost"
	opts.BrainDir = brainDir
	opts.LogLevel = "error"
	h, _, cleanup, err := buildHTTPHandler(ctx, opts)
	if err != nil {
		cancel()
		t.Fatalf("buildHTTPHandler: %v", err)
	}
	return h, func() { cancel(); cleanup() }
}

func calendarRequest(t *testing.T, h http.Handler, method, path, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func listedCalendars(t *testing.T, h http.Handler) map[string]map[string]any {
	t.Helper()
	rec := calendarRequest(t, h, http.MethodGet, "/api/v1/calendars", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/calendars = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Calendars []map[string]any `json:"calendars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := map[string]map[string]any{}
	for _, c := range body.Calendars {
		out[c["name"].(string)] = c
	}
	return out
}

func assertNoRuntimeSecret(t *testing.T, where, text string) {
	t.Helper()
	if strings.Contains(text, runtimeSecretToken) {
		t.Fatalf("%s leaked the calendar address", where)
	}
}

// A runtime source added through the router survives a restart: the second
// boot reads it from the private store, polls it, and lists it with origin
// runtime.
func TestCalendarRuntimeSourceSurvivesRestart(t *testing.T) {
	feedURL := runtimeFeed(t)
	brainDir := filepath.Join(t.TempDir(), "brain")

	first, stopFirst := bootCalendarServer(t, brainDir, ServerOptions{})
	rec := calendarRequest(t, first, http.MethodPost, "/api/v1/calendars",
		`{"name":"personal","url":"`+feedURL+`","poll":"10m"}`, "")
	if rec.Code != http.StatusCreated {
		stopFirst()
		t.Fatalf("POST /api/v1/calendars = %d %s", rec.Code, rec.Body.String())
	}
	assertNoRuntimeSecret(t, "create response", rec.Body.String())
	stopFirst()

	second, stopSecond := bootCalendarServer(t, brainDir, ServerOptions{})
	defer stopSecond()
	got, ok := listedCalendars(t, second)["personal"]
	if !ok {
		t.Fatal("runtime source did not survive the restart")
	}
	if got["origin"] != "runtime" || got["poll"] != "10m" {
		t.Fatalf("restored source = %v", got)
	}
	if n, _ := got["event_count"].(float64); n < 1 {
		t.Fatalf("restored source has no events: %v", got)
	}
}

// The full flow through the real router: add, see the events, save an
// automation that skips on them, refuse removal with the reference, then
// force it. The address never appears in a response or a log line.
func TestCalendarRuntimeEndToEnd(t *testing.T) {
	logs := captureLogs(t)
	feedURL := runtimeFeed(t)
	h, stop := bootCalendarServer(t, filepath.Join(t.TempDir(), "brain"), ServerOptions{})
	defer stop()

	var bodies []string
	record := func(rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		bodies = append(bodies, rec.Body.String())
		return rec
	}

	rec := record(calendarRequest(t, h, http.MethodPost, "/api/v1/calendars",
		`{"name":"personal","url":"`+feedURL+`"}`, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", rec.Code, rec.Body.String())
	}
	if got := listedCalendars(t, h)["personal"]; got == nil || got["event_count"] != float64(1) {
		t.Fatalf("after add, personal = %v; want one event", got)
	}

	automation := `{"type":"automation","title":"Skip on personal","content":"Do it","status":"active",` +
		`"project":"demo","trigger":{"type":"cron","schedule":"0 9 * * *","skip_if_event":{"calendar":"personal"}},` +
		`"action":{"type":"prompt","direct_prompt":"run"}}`
	rec = record(calendarRequest(t, h, http.MethodPost, "/api/v1/entries", automation, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("save automation = %d %s", rec.Code, rec.Body.String())
	}

	rec = record(calendarRequest(t, h, http.MethodDelete, "/api/v1/calendars/personal", "", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("referenced removal = %d %s; want 409", rec.Code, rec.Body.String())
	}
	var refused struct {
		References []struct {
			ID    string `json:"id"`
			Field string `json:"field"`
		} `json:"references"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refused); err != nil {
		t.Fatalf("decode 409: %v", err)
	}
	if len(refused.References) != 1 || refused.References[0].Field != "trigger.skip_if_event" || refused.References[0].ID == "" {
		t.Fatalf("409 references = %+v", refused.References)
	}

	rec = record(calendarRequest(t, h, http.MethodDelete, "/api/v1/calendars/personal?force=true", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("forced removal = %d %s", rec.Code, rec.Body.String())
	}
	if _, still := listedCalendars(t, h)["personal"]; still {
		t.Fatal("forced removal left the source listed")
	}

	rec = record(calendarRequest(t, h, http.MethodPost, "/api/v1/entries",
		strings.Replace(automation, "Skip on personal", "Again", 1), ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("saving an automation with the removed name = %d %s; want 400", rec.Code, rec.Body.String())
	}

	for i, body := range bodies {
		assertNoRuntimeSecret(t, fmt.Sprintf("response %d", i), body)
	}
	assertNoRuntimeSecret(t, "captured logs", logs.String())
	if !strings.Contains(logs.String(), "calendar source added") {
		t.Fatalf("the add was not logged; logs:\n%s", logs.String())
	}
}

// With auth on, the admin write routes refuse a read-only token and accept an
// admin token.
func TestCalendarRuntimeWritesRequireAdminOverTheRealRouter(t *testing.T) {
	t.Setenv("BRAIN_ALLOW_REMOTE_BOOTSTRAP", "")
	feedURL := runtimeFeed(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, _, cleanup, err := buildHTTPHandler(ctx, ServerOptions{
		Host: "localhost", BrainDir: filepath.Join(t.TempDir(), "brain"), LogLevel: "error", EnableAuth: true,
	})
	if err != nil {
		t.Fatalf("buildHTTPHandler: %v", err)
	}
	defer cleanup()

	bootstrap := httptest.NewRequest(http.MethodPost, "/api/v1/tokens/bootstrap", strings.NewReader(`{"name":"first"}`))
	bootstrap.RemoteAddr = "127.0.0.1:1234"
	bootstrap.Header.Set("Content-Type", "application/json")
	bootRec := httptest.NewRecorder()
	h.ServeHTTP(bootRec, bootstrap)
	if bootRec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d %s", bootRec.Code, bootRec.Body.String())
	}
	var admin struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(bootRec.Body.Bytes(), &admin); err != nil || admin.Token == "" {
		t.Fatalf("bootstrap token: %v", err)
	}

	reader := calendarRequest(t, h, http.MethodPost, "/api/v1/tokens", `{"name":"reader","scope":"read:*"}`, admin.Token)
	if reader.Code != http.StatusCreated {
		t.Fatalf("reader token = %d %s", reader.Code, reader.Body.String())
	}
	var readerToken struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(reader.Body.Bytes(), &readerToken); err != nil {
		t.Fatal(err)
	}

	body := `{"name":"personal","url":"` + feedURL + `"}`
	if rec := calendarRequest(t, h, http.MethodPost, "/api/v1/calendars", body, readerToken.Token); rec.Code != http.StatusForbidden {
		t.Fatalf("read token create = %d %s; want 403", rec.Code, rec.Body.String())
	}
	if rec := calendarRequest(t, h, http.MethodPost, "/api/v1/calendars", body, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous create = %d; want 401", rec.Code)
	}
	rec := calendarRequest(t, h, http.MethodPost, "/api/v1/calendars", body, admin.Token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create = %d %s; want 201", rec.Code, rec.Body.String())
	}
	assertNoRuntimeSecret(t, "admin create response", rec.Body.String())
}

// Tenant mode never builds a calendar manager, so the write routes answer 501
// on the tenant graph's handler.
func TestTenantGraphCalendarWritesAreNotImplemented(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	if _, err := views.roots.ProvisionLocal(ctx, root, filepath.Join(root, "custom-blobs")); err != nil {
		t.Fatal(err)
	}
	g, err := newTenantGraph(ctx, views.tenant, views.roots, calendarGraphConfig(t, nil), graphIdentity{})
	if err != nil {
		t.Fatalf("construct tenant graph: %v", err)
	}
	defer g.Close()

	router := api.NewRouter(config.Config{}, api.WithHandler(g.handler))
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/calendars", `{"name":"a","url":"https://example.test/x.ics"}`},
		{http.MethodPut, "/api/v1/calendars/a", `{"poll":"10m"}`},
		{http.MethodDelete, "/api/v1/calendars/a", ""},
	} {
		rec := calendarRequest(t, router, tc.method, tc.path, tc.body, "")
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("tenant %s %s = %d %s; want 501", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}
