package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/config"
)

// fakeCalendars is a CalendarService with canned statuses.
type fakeCalendars struct {
	statuses []calendar.Status
}

func (f fakeCalendars) Statuses() []calendar.Status { return f.statuses }

func calendarsRouter(svc CalendarService) http.Handler {
	opts := []HandlerOption{}
	if svc != nil {
		opts = append(opts, WithCalendarService(svc))
	}
	return NewRouter(config.Config{}, WithHandler(NewHandler(&mockBrainService{}, opts...)))
}

func getCalendars(t *testing.T, router http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/calendars", nil))
	return rec
}

type calendarsBody struct {
	Calendars []map[string]any `json:"calendars"`
}

func decodeCalendars(t *testing.T, rec *httptest.ResponseRecorder) calendarsBody {
	t.Helper()
	var body calendarsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestCalendars_ListsEverySourceWithoutURLsOrPaths(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	svc := fakeCalendars{statuses: []calendar.Status{
		{Name: "team", Kind: calendar.KindICS, LastFetch: now, LastSuccess: now.Add(-time.Hour), EventCount: 3, LastError: "HTTP 503", Stale: true},
		{Name: "xnys", Kind: calendar.KindBuiltin},
	}}

	rec := getCalendars(t, calendarsRouter(svc))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if raw := rec.Body.String(); strings.Contains(raw, "://") || strings.Contains(raw, ".ics") {
		t.Fatalf("response carries a URL or feed path: %s", raw)
	}
	body := decodeCalendars(t, rec)
	if len(body.Calendars) != 2 {
		t.Fatalf("calendars: got %d want 2: %s", len(body.Calendars), rec.Body.String())
	}

	team := body.Calendars[0]
	if team["name"] != "team" || team["kind"] != calendar.KindICS {
		t.Fatalf("ics entry identity: %+v", team)
	}
	if team["event_count"] != float64(3) || team["stale"] != true || team["last_error"] != "HTTP 503" {
		t.Fatalf("ics entry state: %+v", team)
	}
	if _, ok := team["last_fetch"]; !ok {
		t.Fatalf("ics entry missing last_fetch: %+v", team)
	}
	if _, ok := team["last_success"]; !ok {
		t.Fatalf("ics entry missing last_success: %+v", team)
	}

	xnys := body.Calendars[1]
	if xnys["name"] != "xnys" || xnys["kind"] != calendar.KindBuiltin {
		t.Fatalf("builtin entry identity: %+v", xnys)
	}
	for key := range xnys {
		if key != "name" && key != "kind" {
			t.Fatalf("builtin entry carries fetch field %q: %+v", key, xnys)
		}
	}
}

func TestCalendars_NeverFetchedIcsOmitsFetchTimes(t *testing.T) {
	svc := fakeCalendars{statuses: []calendar.Status{{Name: "team", Kind: calendar.KindICS}}}

	body := decodeCalendars(t, getCalendars(t, calendarsRouter(svc)))
	if len(body.Calendars) != 1 {
		t.Fatalf("calendars: %+v", body.Calendars)
	}
	entry := body.Calendars[0]
	for _, key := range []string{"last_fetch", "last_success", "last_error"} {
		if _, ok := entry[key]; ok {
			t.Fatalf("never-fetched source reports %q: %+v", key, entry)
		}
	}
	if entry["event_count"] != float64(0) || entry["stale"] != false {
		t.Fatalf("never-fetched source state: %+v", entry)
	}
}

func TestCalendars_NoSourcesIsAnEmptyListNotNull(t *testing.T) {
	rec := getCalendars(t, calendarsRouter(fakeCalendars{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"calendars":[]`) {
		t.Fatalf("empty list must be [], got %s", rec.Body.String())
	}
}

func TestCalendars_UnconfiguredServiceIsNotImplemented(t *testing.T) {
	rec := getCalendars(t, calendarsRouter(nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status %d want 501: %s", rec.Code, rec.Body.String())
	}
}
