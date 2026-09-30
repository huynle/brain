package runner

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// v2 message reads: GET /api/session/{id}/message returns
// {"data":[flat messages],"cursor":{previous,next}} with Basic auth.
// The runner normalizes each v2 message into its internal {info,parts}
// shape (info.role from v2 type, parts from v2 content) so turn-end
// detection and the frontend transcript keep working.

func TestNormalizeV2Messages_ShapeAndRoleAndParts(t *testing.T) {
	raw := []byte(`{"data":[
		{"id":"msg_u","type":"user","time":{"created":100},"text":"hi"},
		{"id":"msg_a","type":"assistant","time":{"created":200,"completed":300},"content":[{"type":"text","text":"hello"}]},
		{"id":"msg_i","type":"idle","time":{"created":400},"outcome":"succeeded"}
	],"cursor":{"previous":null,"next":null}}`)

	out, err := normalizeV2Messages(raw)
	if err != nil {
		t.Fatalf("normalizeV2Messages: %v", err)
	}
	var msgs []messageWithParts
	if err := json.Unmarshal(out, &msgs); err != nil {
		t.Fatalf("normalized output not {info,parts} array: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	// Assistant message: info.role == assistant, time.completed carried,
	// parts from content.
	var info struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		Time struct {
			Created   float64 `json:"created"`
			Completed float64 `json:"completed"`
		} `json:"time"`
	}
	if err := json.Unmarshal(msgs[1].Info, &info); err != nil {
		t.Fatalf("decode assistant info: %v", err)
	}
	if info.Role != "assistant" {
		t.Fatalf("assistant role = %q, want assistant (mapped from v2 type)", info.Role)
	}
	if info.Time.Completed != 300 {
		t.Fatalf("assistant time.completed = %v, want 300", info.Time.Completed)
	}
	if len(msgs[1].Parts) != 1 {
		t.Fatalf("assistant parts = %d, want 1 (from content)", len(msgs[1].Parts))
	}
	var part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msgs[1].Parts[0], &part); err != nil {
		t.Fatalf("decode part: %v", err)
	}
	if part.Type != "text" || part.Text != "hello" {
		t.Fatalf("part = %+v, want text/hello", part)
	}
}

func TestFetchSessionMessages_V2_PathAuthUnwrapPaginate(t *testing.T) {
	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session/ses_a/message" {
			t.Fatalf("want /api/session/ses_a/message, got %s", r.URL.Path)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:pwM"))
		if got := r.Header.Get("Authorization"); got != want {
			t.Fatalf("auth = %q, want %q", got, want)
		}
		pages++
		w.Header().Set("Content-Type", "application/json")
		cursor := r.URL.Query().Get("cursor")
		if cursor == "" {
			// First page: one user msg, cursor.next points to page 2.
			_, _ = w.Write([]byte(`{"data":[{"id":"m1","type":"user","time":{"created":1},"text":"a"}],"cursor":{"previous":null,"next":"c2"}}`))
			return
		}
		// Second page: the assistant msg, no further next.
		_, _ = w.Write([]byte(`{"data":[{"id":"m2","type":"assistant","time":{"created":2,"completed":3},"content":[{"type":"text","text":"b"}]}],"cursor":{"previous":"c1","next":null}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	out, err := fetchSessionMessages(port, "ses_a", "pwM")
	if err != nil {
		t.Fatalf("fetchSessionMessages: %v", err)
	}
	if pages != 2 {
		t.Fatalf("pages fetched = %d, want 2 (cursor pagination)", pages)
	}
	var msgs []messageWithParts
	if err := json.Unmarshal(out, &msgs); err != nil {
		t.Fatalf("output not {info,parts}: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2 (both pages merged)", len(msgs))
	}
}

func TestCheckOpencodeTurnEnded_V2_AssistantCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"m1","type":"user","time":{"created":1000},"text":"go"},
			{"id":"m2","type":"assistant","time":{"created":2000,"completed":3000},"content":[{"type":"text","text":"done"}]}
		],"cursor":{"previous":null,"next":null}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	ended, last, ok := checkOpencodeTurnEnded(port, "ses_a", "pwT")
	if !ok {
		t.Fatal("ok = false, want true (assistant present)")
	}
	if !ended {
		t.Fatal("ended = false, want true (assistant time.completed set)")
	}
	if last.IsZero() {
		t.Fatal("lastActivity is zero, want newest timestamp")
	}
}

func TestCheckOpencodeTurnEnded_V2_AssistantStillRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"m2","type":"assistant","time":{"created":2000},"content":[{"type":"text","text":"working"}]}
		],"cursor":{"previous":null,"next":null}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	ended, _, ok := checkOpencodeTurnEnded(port, "ses_a", "pwT")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if ended {
		t.Fatal("ended = true, want false (assistant has no time.completed)")
	}
}
