package runner

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// v2 moved every route under /api and requires HTTP Basic auth. These tests
// pin session create + discovery to the v2 contract:
//   POST /api/session  body {}      -> {"data":{"id":...}}
//   GET  /api/session               -> {"data":[{id,projectID,agent,model{...}}]}

func wantBasicAuth(t *testing.T, r *http.Request, pw string) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:"+pw))
	if got := r.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestCreateOpencodeSession_V2_PathAuthUnwrap(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session" {
			t.Fatalf("want POST /api/session, got %s %s", r.Method, r.URL.Path)
		}
		wantBasicAuth(t, r, "pw4")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"ses_created","projectID":"p1","time":{"created":1,"updated":1}}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	id, err := createOpencodeSession(port, "My Task", "pw4")
	if err != nil {
		t.Fatalf("createOpencodeSession: %v", err)
	}
	if id != "ses_created" {
		t.Fatalf("id = %q, want ses_created", id)
	}
	// Body must be a JSON object (v2 requires body; title optional).
	if !strings.HasPrefix(strings.TrimSpace(gotBody), "{") {
		t.Fatalf("body = %q, want JSON object", gotBody)
	}
}

func TestFetchSessions_V2_UnwrapDataAndFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/session" {
			t.Fatalf("want GET /api/session, got %s %s", r.Method, r.URL.Path)
		}
		wantBasicAuth(t, r, "pw5")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"ses_a","parentID":"ses_root","projectID":"proj","agent":"build","model":{"id":"m","providerID":"prov","variant":"v"},"time":{"created":100,"updated":200}},
			{"id":"ses_b","projectID":"proj","time":{"created":50,"updated":60}}
		],"cursor":{"previous":null,"next":null}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	sessions, err := fetchSessions(port, "pw5")
	if err != nil {
		t.Fatalf("fetchSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len = %d, want 2", len(sessions))
	}
	if sessions[0].ID != "ses_a" || sessions[0].ParentID != "ses_root" {
		t.Fatalf("session[0] = %+v", sessions[0])
	}
	if sessions[0].order() != 100 {
		t.Fatalf("order = %d, want 100 (time.created)", sessions[0].order())
	}
}

func TestListSessionIDs_V2(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session" {
			t.Fatalf("want /api/session, got %s", r.URL.Path)
		}
		wantBasicAuth(t, r, "pw6")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"ses_x","projectID":"p","time":{"created":1}}],"cursor":{}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	ids, err := listSessionIDs(port, "pw6")
	if err != nil {
		t.Fatalf("listSessionIDs: %v", err)
	}
	if _, ok := ids["ses_x"]; !ok {
		t.Fatalf("ids = %v, want ses_x", ids)
	}
}

func TestDiscoverSessionID_V2(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session" {
			t.Fatalf("want /api/session, got %s", r.URL.Path)
		}
		wantBasicAuth(t, r, "pw7")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"ses_old","projectID":"p","time":{"created":100}},
			{"id":"ses_new","projectID":"p","time":{"created":200}}
		],"cursor":{}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	// Exclude the pre-existing old session; discovery picks the remaining one.
	id, err := discoverSessionID(port, map[string]struct{}{"ses_old": {}}, "pw7")
	if err != nil {
		t.Fatalf("discoverSessionID: %v", err)
	}
	if id != "ses_new" {
		t.Fatalf("id = %q, want ses_new", id)
	}
}
