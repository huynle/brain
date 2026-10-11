package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// calendarSecret is a stand-in for a Google iCal address. No tool result or
// error may contain it.
const calendarSecret = "https://calendar.example.test/private/s3cret-token-mcp/basic.ics"

// calendarServer records each request and answers with the canned status and
// body. The recorded body is the request body, so tests can check what was
// sent.
type calendarServer struct {
	status   int
	response string
	seen     []string
	bodies   []string
}

func (c *calendarServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c.seen = append(c.seen, r.Method+" "+r.URL.RequestURI())
		c.bodies = append(c.bodies, string(raw))
		status := c.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(c.response))
	}
}

func newCalendarTools(t *testing.T, srv *calendarServer) *Server {
	t.Helper()
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	s := NewServer()
	RegisterCalendarTools(s, NewAPIClient(ts.URL))
	return s
}

// Registration

func TestRegisterCalendarTools_Names(t *testing.T) {
	s := NewServer()
	RegisterCalendarTools(s, NewAPIClient("http://localhost:3333"))
	want := []string{"calendars_list", "calendar_add", "calendar_update", "calendar_remove"}
	if len(s.tools) != len(want) {
		t.Fatalf("registered %d calendar tools, want %d", len(s.tools), len(want))
	}
	for _, name := range want {
		rt, ok := s.tools[name]
		if !ok {
			t.Fatalf("tool %q not registered", name)
		}
		if rt.handler == nil || rt.tool.Description == "" {
			t.Fatalf("tool %q lacks a handler or description", name)
		}
		if rt.tool.InputSchema.Type != "object" {
			t.Fatalf("tool %q schema type = %q", name, rt.tool.InputSchema.Type)
		}
	}
}

func TestCalendarToolDescriptionsSayTheAddressIsASecret(t *testing.T) {
	s := NewServer()
	RegisterCalendarTools(s, NewAPIClient("http://localhost:3333"))
	for _, name := range []string{"calendar_add", "calendar_update", "calendar_remove"} {
		desc := strings.ToLower(s.tools[name].tool.Description)
		for _, phrase := range []string{"secret", "never shown again", "not be repeated"} {
			if !strings.Contains(desc, phrase) {
				t.Errorf("%s description lacks %q: %s", name, phrase, s.tools[name].tool.Description)
			}
		}
	}
	if desc := strings.ToLower(s.tools["calendars_list"].tool.Description); strings.Contains(desc, "address") && !strings.Contains(desc, "never") {
		t.Errorf("calendars_list must say it never returns an address: %s", s.tools["calendars_list"].tool.Description)
	}
}

func TestCalendarToolSchemas(t *testing.T) {
	s := NewServer()
	RegisterCalendarTools(s, NewAPIClient("http://localhost:3333"))
	required := func(name string) []string { return s.tools[name].tool.InputSchema.Required }
	props := func(name string) []string {
		var out []string
		for k := range s.tools[name].tool.InputSchema.Properties {
			out = append(out, k)
		}
		return out
	}
	if len(required("calendars_list")) != 0 {
		t.Fatalf("calendars_list requires %v", required("calendars_list"))
	}
	if got := required("calendar_add"); !reflect.DeepEqual(got, []string{"name", "url"}) {
		t.Fatalf("calendar_add required = %v", got)
	}
	if got := required("calendar_update"); !reflect.DeepEqual(got, []string{"name"}) {
		t.Fatalf("calendar_update required = %v", got)
	}
	if got := required("calendar_remove"); !reflect.DeepEqual(got, []string{"name"}) {
		t.Fatalf("calendar_remove required = %v", got)
	}
	for _, p := range []string{"name", "url", "poll"} {
		if !listHas(props("calendar_add"), p) {
			t.Errorf("calendar_add lacks property %q", p)
		}
	}
	if !listHas(props("calendar_remove"), "force") {
		t.Error("calendar_remove lacks property force")
	}
}

func listHas(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// calendars_list

func TestCalendarsList_ShowsOriginPollAndNoAddress(t *testing.T) {
	srv := &calendarServer{response: `{"calendars":[` +
		`{"name":"personal","kind":"ics","origin":"runtime","poll":"5m","event_count":3,"stale":false},` +
		`{"name":"team","kind":"ics","origin":"config","poll":"1h","event_count":0,"last_error":"HTTP 503","stale":true},` +
		`{"name":"xnys","kind":"builtin","origin":"config"}]}`}
	s := newCalendarTools(t, srv)
	out, err := s.tools["calendars_list"].handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"personal", "runtime", "5m", "team", "config", "1h", "HTTP 503", "xnys", "builtin"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing lacks %q:\n%s", want, out)
		}
	}
	if want := []string{"GET /api/v1/calendars"}; !reflect.DeepEqual(srv.seen, want) {
		t.Fatalf("routes = %v, want %v", srv.seen, want)
	}
}

// calendar_add

func TestCalendarAdd_SendsTheAddressOnceAndReturnsNoAddress(t *testing.T) {
	srv := &calendarServer{status: http.StatusCreated, response: `{"name":"personal","kind":"ics","origin":"runtime","poll":"5m","event_count":2}`}
	s := newCalendarTools(t, srv)
	out, err := s.tools["calendar_add"].handler(context.Background(), map[string]any{
		"name": "personal", "url": calendarSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"POST /api/v1/calendars"}; !reflect.DeepEqual(srv.seen, want) {
		t.Fatalf("routes = %v, want %v", srv.seen, want)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(srv.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if sent["url"] != calendarSecret || sent["name"] != "personal" {
		t.Fatalf("request body = %v", sent)
	}
	if _, has := sent["poll"]; has {
		t.Fatalf("unset poll was sent: %v", sent)
	}
	if strings.Contains(out, "s3cret-token-mcp") || strings.Contains(out, "calendar.example.test") {
		t.Fatalf("tool result echoed the address: %s", out)
	}
	if !strings.Contains(out, "personal") || !strings.Contains(out, "2") {
		t.Fatalf("result does not confirm the source: %s", out)
	}
}

func TestCalendarAdd_SendsAnExplicitPoll(t *testing.T) {
	srv := &calendarServer{status: http.StatusCreated, response: `{"name":"personal","kind":"ics","origin":"runtime","poll":"10m"}`}
	s := newCalendarTools(t, srv)
	if _, err := s.tools["calendar_add"].handler(context.Background(), map[string]any{
		"name": "personal", "url": calendarSecret, "poll": "10m",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(srv.bodies[0], `"poll":"10m"`) {
		t.Fatalf("poll not sent: %s", srv.bodies[0])
	}
}

func TestCalendarAdd_RequiresNameAndURLWithoutCalling(t *testing.T) {
	srv := &calendarServer{}
	s := newCalendarTools(t, srv)
	for _, args := range []map[string]any{
		{"url": calendarSecret},
		{"name": "personal"},
	} {
		if _, err := s.tools["calendar_add"].handler(context.Background(), args); err == nil {
			t.Fatalf("args %v accepted", args)
		}
	}
	if len(srv.seen) != 0 {
		t.Fatalf("server called for an incomplete add: %v", srv.seen)
	}
}

func TestCalendarAdd_ErrorNeverEchoesTheAddress(t *testing.T) {
	srv := &calendarServer{status: http.StatusUnprocessableEntity, response: `{"error":"Unprocessable Entity","message":"calendar address rejected: feed host resolves to a non-public address"}`}
	s := newCalendarTools(t, srv)
	_, err := s.tools["calendar_add"].handler(context.Background(), map[string]any{"name": "personal", "url": calendarSecret})
	if err == nil {
		t.Fatal("rejected address returned no error")
	}
	if strings.Contains(err.Error(), "s3cret-token-mcp") {
		t.Fatalf("error echoed the address: %v", err)
	}
}

// calendar_update

func TestCalendarUpdate_PollOnlySendsNoAddress(t *testing.T) {
	srv := &calendarServer{response: `{"name":"personal","kind":"ics","origin":"runtime","poll":"15m"}`}
	s := newCalendarTools(t, srv)
	out, err := s.tools["calendar_update"].handler(context.Background(), map[string]any{"name": "personal", "poll": "15m"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"PUT /api/v1/calendars/personal"}; !reflect.DeepEqual(srv.seen, want) {
		t.Fatalf("routes = %v", srv.seen)
	}
	if strings.Contains(srv.bodies[0], `"url"`) {
		t.Fatalf("poll-only update sent an address: %s", srv.bodies[0])
	}
	if !strings.Contains(out, "15m") {
		t.Fatalf("result lacks the new poll: %s", out)
	}
}

func TestCalendarUpdate_ReplacesTheAddressWithoutEchoingIt(t *testing.T) {
	srv := &calendarServer{response: `{"name":"personal","kind":"ics","origin":"runtime","poll":"5m"}`}
	s := newCalendarTools(t, srv)
	out, err := s.tools["calendar_update"].handler(context.Background(), map[string]any{"name": "personal", "url": calendarSecret})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(srv.bodies[0], "s3cret-token-mcp") {
		t.Fatalf("replacement address not sent: %s", srv.bodies[0])
	}
	if strings.Contains(out, "s3cret-token-mcp") {
		t.Fatalf("tool result echoed the address: %s", out)
	}
}

func TestCalendarUpdate_RequiresURLOrPollWithoutCalling(t *testing.T) {
	srv := &calendarServer{}
	s := newCalendarTools(t, srv)
	if _, err := s.tools["calendar_update"].handler(context.Background(), map[string]any{"name": "personal"}); err == nil {
		t.Fatal("update with neither url nor poll accepted")
	}
	if len(srv.seen) != 0 {
		t.Fatalf("server called for an empty update: %v", srv.seen)
	}
}

// calendar_remove

func TestCalendarRemove_ForceAndReferences(t *testing.T) {
	srv := &calendarServer{response: `{"name":"personal","removed":true,"references":[{"id":"a1","path":"projects/p/automation/a1.md","title":"Morning gate","field":"trigger.skip_if_event"}]}`}
	s := newCalendarTools(t, srv)
	out, err := s.tools["calendar_remove"].handler(context.Background(), map[string]any{"name": "personal", "force": true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"DELETE /api/v1/calendars/personal?force=true"}; !reflect.DeepEqual(srv.seen, want) {
		t.Fatalf("routes = %v, want %v", srv.seen, want)
	}
	if !strings.Contains(out, "removed") || !strings.Contains(out, "Morning gate") {
		t.Fatalf("result lacks the removal and its references: %s", out)
	}
}

func TestCalendarRemove_WithoutForceSendsNoFlag(t *testing.T) {
	srv := &calendarServer{response: `{"name":"personal","removed":true,"references":[]}`}
	s := newCalendarTools(t, srv)
	if _, err := s.tools["calendar_remove"].handler(context.Background(), map[string]any{"name": "personal"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"DELETE /api/v1/calendars/personal"}; !reflect.DeepEqual(srv.seen, want) {
		t.Fatalf("routes = %v, want %v", srv.seen, want)
	}
}

func TestCalendarRemove_RefusedWhileReferenced(t *testing.T) {
	srv := &calendarServer{status: http.StatusConflict, response: `{"error":"Conflict","message":"calendar is referenced by automations; remove with force=true to proceed","references":[{"id":"a1","path":"p","title":"t","field":"trigger.skip_if_event"}]}`}
	s := newCalendarTools(t, srv)
	_, err := s.tools["calendar_remove"].handler(context.Background(), map[string]any{"name": "personal"})
	if err == nil || !strings.Contains(err.Error(), "referenced by automations") {
		t.Fatalf("refusal = %v; want the referenced-by-automations message", err)
	}
}
