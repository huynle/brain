package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/config"
)

// secretFeed stands in for a Google iCal address. It must never appear in any
// response body, which these tests check for every case.
const secretFeed = "https://calendar.example.test/private/s3cret-token-xyz/basic.ics"

// fakeCalendarManager records each write and returns canned results.
type fakeCalendarManager struct {
	calls int

	addReq    calendar.AddRequest
	addActor  string
	addStatus calendar.Status
	addErr    error

	updName   string
	updReq    calendar.UpdateRequest
	updActor  string
	updStatus calendar.Status
	updErr    error

	remName   string
	remForce  bool
	remActor  string
	remResult calendar.RemoveResult
	remErr    error
}

func (f *fakeCalendarManager) Add(_ context.Context, req calendar.AddRequest, actor string) (calendar.Status, error) {
	f.calls++
	f.addReq, f.addActor = req, actor
	return f.addStatus, f.addErr
}

func (f *fakeCalendarManager) Update(_ context.Context, name string, req calendar.UpdateRequest, actor string) (calendar.Status, error) {
	f.calls++
	f.updName, f.updReq, f.updActor = name, req, actor
	return f.updStatus, f.updErr
}

func (f *fakeCalendarManager) Remove(_ context.Context, name string, force bool, actor string) (calendar.RemoveResult, error) {
	f.calls++
	f.remName, f.remForce, f.remActor = name, force, actor
	return f.remResult, f.remErr
}

// writeRouter serves the real router over a handler with mgr attached (nil
// means tenant mode: no manager). When auth is set, every request carries it.
func writeRouter(mgr CalendarManager, auth *AuthResult) http.Handler {
	opts := []HandlerOption{}
	if mgr != nil {
		opts = append(opts, WithCalendarManager(mgr))
	}
	router := NewRouter(config.Config{}, WithHandler(NewHandler(&mockBrainService{}, opts...)))
	if auth == nil {
		return router
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxAuthResult, auth)))
	})
}

var adminCaller = &AuthResult{Type: "api_token", Name: "ops", Scope: "admin:*"}

func calendarCall(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertNoSecret(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if strings.Contains(rec.Body.String(), "s3cret-token-xyz") || strings.Contains(rec.Body.String(), "calendar.example.test") {
		t.Fatalf("response leaked the calendar address: %s", rec.Body.String())
	}
}

func runtimeICS(name string, poll time.Duration) calendar.Status {
	return calendar.Status{Name: name, Kind: calendar.KindICS, Origin: calendar.OriginRuntime, Poll: poll}
}

func TestCalendarWrite_CreateReturns201WithoutTheAddress(t *testing.T) {
	mgr := &fakeCalendarManager{addStatus: runtimeICS("personal", 5*time.Minute)}
	rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodPost, "/api/v1/calendars",
		`{"name":"personal","url":"`+secretFeed+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	assertNoSecret(t, rec)
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view["name"] != "personal" || view["kind"] != calendar.KindICS || view["origin"] != calendar.OriginRuntime || view["poll"] != "5m" {
		t.Fatalf("view = %v", view)
	}
	if mgr.addReq.URL != secretFeed || mgr.addReq.Name != "personal" || mgr.addReq.Poll != 0 {
		t.Fatalf("manager received %+v", mgr.addReq)
	}
	if mgr.addActor != "ops" {
		t.Fatalf("actor = %q, want the caller's name", mgr.addActor)
	}
}

func TestCalendarWrite_CreatePassesAnExplicitPoll(t *testing.T) {
	mgr := &fakeCalendarManager{addStatus: runtimeICS("personal", 10*time.Minute)}
	rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodPost, "/api/v1/calendars",
		`{"name":"personal","url":"`+secretFeed+`","poll":"10m"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if mgr.addReq.Poll != 10*time.Minute {
		t.Fatalf("poll = %v, want 10m", mgr.addReq.Poll)
	}
}

func TestCalendarWrite_CreateRejectsBadRequestsBeforeTheManager(t *testing.T) {
	cases := map[string]string{
		"invalid JSON":   `{"name":`,
		"unknown field":  `{"name":"a","url":"` + secretFeed + `","colour":"red"}`,
		"empty body":     ``,
		"trailing value": `{"name":"a","url":"` + secretFeed + `"} {}`,
		"bad poll":       `{"name":"a","url":"` + secretFeed + `","poll":"soon"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			mgr := &fakeCalendarManager{}
			rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodPost, "/api/v1/calendars", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if mgr.calls != 0 {
				t.Fatalf("manager called %d times for a malformed request", mgr.calls)
			}
			assertNoSecret(t, rec)
		})
	}
}

func TestCalendarWrite_CreateMapsManagerErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"invalid name", calendar.ErrInvalidName, http.StatusBadRequest},
		{"invalid poll", calendar.ErrInvalidPoll, http.StatusBadRequest},
		{"exists", calendar.ErrExists, http.StatusConflict},
		{"limit", calendar.ErrLimit, http.StatusConflict},
		{"config source", calendar.ErrConfigSource, http.StatusConflict},
		{"rejected address", calendar.ErrRejectedURL, http.StatusUnprocessableEntity},
		{"unexpected", errors.New("disk full"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &fakeCalendarManager{addErr: tc.err}
			rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodPost, "/api/v1/calendars",
				`{"name":"personal","url":"`+secretFeed+`"}`)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			assertNoSecret(t, rec)
		})
	}
}

func TestCalendarWrite_UpdateChangesPollOrAddress(t *testing.T) {
	mgr := &fakeCalendarManager{updStatus: runtimeICS("personal", 10*time.Minute)}
	h := writeRouter(mgr, adminCaller)

	rec := calendarCall(t, h, http.MethodPut, "/api/v1/calendars/personal", `{"poll":"10m"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("poll update status %d: %s", rec.Code, rec.Body.String())
	}
	if mgr.updName != "personal" || mgr.updReq.Poll == nil || *mgr.updReq.Poll != 10*time.Minute || mgr.updReq.URL != nil {
		t.Fatalf("poll update reached manager as name=%q req=%+v", mgr.updName, mgr.updReq)
	}
	if mgr.updActor != "ops" {
		t.Fatalf("actor = %q", mgr.updActor)
	}

	rec = calendarCall(t, h, http.MethodPut, "/api/v1/calendars/personal", `{"url":"`+secretFeed+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("address update status %d: %s", rec.Code, rec.Body.String())
	}
	assertNoSecret(t, rec)
	if mgr.updReq.URL == nil || *mgr.updReq.URL != secretFeed || mgr.updReq.Poll != nil {
		t.Fatalf("address update reached manager as %+v", mgr.updReq)
	}
}

func TestCalendarWrite_UpdateStatusCodes(t *testing.T) {
	cases := []struct {
		name string
		body string
		err  error
		want int
	}{
		{"nothing to update", `{}`, calendar.ErrNothingToUpdate, http.StatusBadRequest},
		{"empty poll", `{"poll":""}`, nil, http.StatusBadRequest},
		{"bad poll JSON type", `{"poll":5}`, nil, http.StatusBadRequest},
		{"invalid poll", `{"poll":"30s"}`, calendar.ErrInvalidPoll, http.StatusBadRequest},
		{"unknown name", `{"poll":"10m"}`, calendar.ErrNotFound, http.StatusNotFound},
		{"config source", `{"poll":"10m"}`, calendar.ErrConfigSource, http.StatusConflict},
		{"rejected address", `{"url":"` + secretFeed + `"}`, calendar.ErrRejectedURL, http.StatusUnprocessableEntity},
		{"invalid JSON", `{"poll":`, nil, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &fakeCalendarManager{updErr: tc.err}
			rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodPut, "/api/v1/calendars/personal", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			assertNoSecret(t, rec)
		})
	}
}

func TestCalendarWrite_DeleteReturnsEmptyReferencesAsArray(t *testing.T) {
	mgr := &fakeCalendarManager{}
	rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodDelete, "/api/v1/calendars/personal", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"references":[]`) {
		t.Fatalf("references must be an empty array, got %s", rec.Body.String())
	}
	if mgr.remName != "personal" || mgr.remForce {
		t.Fatalf("remove reached manager as name=%q force=%v", mgr.remName, mgr.remForce)
	}
	if mgr.remActor != "ops" {
		t.Fatalf("actor = %q", mgr.remActor)
	}
	assertNoSecret(t, rec)
}

func TestCalendarWrite_ForcedDeleteReportsRemovedReferences(t *testing.T) {
	refs := []calendar.Reference{{ID: "a1", Path: "projects/p/automation/a1.md", Title: "Morning", Field: "trigger.skip_if_event"}}
	mgr := &fakeCalendarManager{remResult: calendar.RemoveResult{References: refs}}
	rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodDelete, "/api/v1/calendars/personal?force=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !mgr.remForce {
		t.Fatal("force=true did not reach the manager")
	}
	var body struct {
		Name       string `json:"name"`
		Removed    bool   `json:"removed"`
		References []struct {
			ID    string `json:"id"`
			Path  string `json:"path"`
			Title string `json:"title"`
			Field string `json:"field"`
		} `json:"references"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Name != "personal" || !body.Removed || len(body.References) != 1 ||
		body.References[0].ID != "a1" || body.References[0].Field != "trigger.skip_if_event" || body.References[0].Path == "" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	assertNoSecret(t, rec)
}

func TestCalendarWrite_DeleteReferencedIsConflictWithReferences(t *testing.T) {
	refs := []calendar.Reference{{ID: "a1", Path: "projects/p/automation/a1.md", Title: "Morning", Field: "trigger.only_if_event"}}
	mgr := &fakeCalendarManager{remErr: &calendar.ReferencedError{References: refs}}
	rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodDelete, "/api/v1/calendars/personal", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		References []map[string]string `json:"references"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.References) != 1 || body.References[0]["id"] != "a1" ||
		body.References[0]["field"] != "trigger.only_if_event" || body.References[0]["title"] != "Morning" ||
		body.References[0]["path"] == "" {
		t.Fatalf("409 body must list references with id/path/title/field: %s", rec.Body.String())
	}
	assertNoSecret(t, rec)
}

func TestCalendarWrite_DeleteStatusCodes(t *testing.T) {
	cases := []struct {
		name string
		url  string
		err  error
		want int
	}{
		{"unknown name", "/api/v1/calendars/nope", calendar.ErrNotFound, http.StatusNotFound},
		{"config source", "/api/v1/calendars/xnys", calendar.ErrConfigSource, http.StatusConflict},
		{"malformed force", "/api/v1/calendars/personal?force=maybe", nil, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &fakeCalendarManager{remErr: tc.err}
			rec := calendarCall(t, writeRouter(mgr, adminCaller), http.MethodDelete, tc.url, "")
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.name == "malformed force" && mgr.calls != 0 {
				t.Fatal("manager called for a malformed force parameter")
			}
		})
	}
}

func TestCalendarWrite_TenantModeIsNotImplemented(t *testing.T) {
	h := writeRouter(nil, adminCaller)
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/calendars", `{"name":"a","url":"` + secretFeed + `"}`},
		{http.MethodPut, "/api/v1/calendars/a", `{"poll":"10m"}`},
		{http.MethodDelete, "/api/v1/calendars/a", ""},
	}
	for _, tc := range cases {
		rec := calendarCall(t, h, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s %s status %d, want 501: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		assertNoSecret(t, rec)
	}
}

func TestCalendarWrite_RequiresAdminScope(t *testing.T) {
	callers := map[string]*AuthResult{
		"read token":   {Type: "api_token", Name: "reader", Scope: "read:*"},
		"runner token": {Type: "api_token", Name: "runner", Scope: "runner:*"},
	}
	for label, caller := range callers {
		mgr := &fakeCalendarManager{}
		h := writeRouter(mgr, caller)
		for _, tc := range []struct{ method, path, body string }{
			{http.MethodPost, "/api/v1/calendars", `{"name":"a","url":"` + secretFeed + `"}`},
			{http.MethodPut, "/api/v1/calendars/a", `{"poll":"10m"}`},
			{http.MethodDelete, "/api/v1/calendars/a", ""},
		} {
			rec := calendarCall(t, h, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s: %s %s status %d, want 403", label, tc.method, tc.path, rec.Code)
			}
		}
		if mgr.calls != 0 {
			t.Fatalf("%s reached the manager %d times", label, mgr.calls)
		}
	}
}

func TestCalendarsViewCarriesOriginAndPoll(t *testing.T) {
	svc := fakeCalendars{statuses: []calendar.Status{
		runtimeICS("personal", 5*time.Minute),
		{Name: "team", Kind: calendar.KindICS, Origin: calendar.OriginConfig, Poll: time.Hour},
		{Name: "xnys", Kind: calendar.KindBuiltin, Origin: calendar.OriginConfig},
	}}
	rec := getCalendars(t, calendarsRouter(svc))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeCalendars(t, rec)
	byName := map[string]map[string]any{}
	for _, c := range body.Calendars {
		byName[c["name"].(string)] = c
	}
	if got := byName["personal"]; got["origin"] != "runtime" || got["poll"] != "5m" {
		t.Fatalf("runtime source = %v", got)
	}
	if got := byName["team"]; got["origin"] != "config" || got["poll"] != "1h" {
		t.Fatalf("config source = %v", got)
	}
	if got := byName["xnys"]; got["origin"] != "config" {
		t.Fatalf("builtin source origin = %v", got["origin"])
	}
	if _, has := byName["xnys"]["poll"]; has {
		t.Fatal("builtin source reports a poll interval")
	}
}

func TestFormatPollUsesCompactGoDurations(t *testing.T) {
	cases := map[time.Duration]string{
		5 * time.Minute:              "5m",
		time.Hour:                    "1h",
		90 * time.Second:             "1m30s",
		10*time.Minute + time.Second: "10m1s",
	}
	for in, want := range cases {
		got := formatPoll(in)
		if got != want {
			t.Errorf("formatPoll(%v) = %q, want %q", in, got, want)
		}
		if back, err := time.ParseDuration(got); err != nil || back != in {
			t.Errorf("formatPoll(%v) = %q does not round-trip: %v %v", in, got, back, err)
		}
	}
}
