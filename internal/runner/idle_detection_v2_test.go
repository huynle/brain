package runner

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// v2 idle detection: /session/status -> /api/session/active (+auth);
// prompt_async -> /api/session/{id}/prompt with body {"text":""};
// abort -> /api/session/{id}/interrupt.

func wantBasicAuthIdle(t *testing.T, r *http.Request, pw string) {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:"+pw))
	if got := r.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestCheckOpencodeStatus_V2_IdleEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session/active" {
			t.Fatalf("want /api/session/active, got %s", r.URL.Path)
		}
		wantBasicAuthIdle(t, r, "pwI")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	if got := checkOpencodeStatus(port, "pwI"); got != "idle" {
		t.Fatalf("status = %q, want idle", got)
	}
}

func TestCheckOpencodeStatus_V2_BusyKeyedBySession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session/active" {
			t.Fatalf("want /api/session/active, got %s", r.URL.Path)
		}
		wantBasicAuthIdle(t, r, "pwB")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ses_x":{"type":"running"}}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	if got := checkOpencodeStatus(port, "pwB"); got != "busy" {
		t.Fatalf("status = %q, want busy", got)
	}
}

func TestPostEmptyPrompt_V2_PathAuthBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session/ses_a/prompt" {
			t.Fatalf("want POST /api/session/ses_a/prompt, got %s %s", r.Method, r.URL.Path)
		}
		wantBasicAuthIdle(t, r, "pwP")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	if err := postEmptyPrompt(port, "ses_a", "pwP"); err != nil {
		t.Fatalf("postEmptyPrompt: %v", err)
	}
	// v2 requires a JSON body with text; empty parts body no longer works.
	if !strings.Contains(gotBody, `"text"`) {
		t.Fatalf("body = %q, want a text field", gotBody)
	}
}

func TestPostAbort_V2_InterruptPathAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/session/ses_a/interrupt" {
			t.Fatalf("want POST /api/session/ses_a/interrupt, got %s %s", r.Method, r.URL.Path)
		}
		wantBasicAuthIdle(t, r, "pwA")
		w.Header().Set("Content-Type", "application/json")
		// v2 interrupt returns {"interrupted":bool}, NOT data-wrapped.
		_, _ = w.Write([]byte(`{"interrupted":true}`))
	}))
	defer srv.Close()
	port := serverPortFromURL(t, srv.URL)

	if err := postAbort(port, "ses_a", "pwA"); err != nil {
		t.Fatalf("postAbort: %v", err)
	}
}
