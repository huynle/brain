package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReaderURLResolvesAndEscapes(t *testing.T) {
	calls := 0
	path := "projects/demo/scratch/a space#&.md"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("auth not forwarded")
		}
		if strings.Contains(strings.ToLower(r.RequestURI), "%2f") {
			t.Error("entry route separators must remain unescaped")
		}
		if r.URL.Query().Get("injected") != "" {
			t.Error("path injected query")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "abcd1234", "path": path, "title": "Reader example"})
	}))
	defer api.Close()
	client := NewAPIClient(api.URL).WithAuthToken("secret")
	s := NewServer()
	registerReaderURL(s, client)
	for _, base := range []string{"", "https://brain.huynle.com", "http://localhost:3333/"} {
		args := map[string]any{"path": "abcd1234?injected=yes"}
		if base != "" {
			args["base_url"] = base
			args["path"] = path
		}
		result, err := s.tools["reader_url"].handler(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]string
		_ = json.Unmarshal([]byte(result), &out)
		u, err := url.Parse(out["reader_url"])
		if err != nil || u.Query().Get("entry") != path || u.Path != "/read.html" || u.Fragment != "" {
			t.Fatal(out, err)
		}
		want := base
		if want == "" {
			want = api.URL
		}
		if u.Scheme+"://"+u.Host != strings.TrimSuffix(want, "/") {
			t.Fatal(out)
		}
	}
	if calls != 3 {
		t.Fatal("override should only use connected API", calls)
	}
	for _, base := range []string{"javascript:alert(1)", "https://secret@example.com", "https://example.com/path", "https://example.com?token=x", "https://example.com#x", "//example.com"} {
		if _, err := s.tools["reader_url"].handler(context.Background(), map[string]any{"path": "abcd1234", "base_url": base}); err == nil {
			t.Fatal("unsafe origin accepted", base)
		}
	}
	if calls != 3 {
		t.Fatal("invalid origins caused API requests")
	}
}
func TestReaderURLHTTPUsesPublicOriginAndPropagatesErrors(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "missing") {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"abcd1234","path":"projects/p/scratch/abcd1234.md","title":"Note"}`))
	}))
	defer api.Close()
	shared := NewAPIClient(api.URL)
	h := NewHTTPHandler(shared)
	for _, path := range []string{"abcd1234", "missing"} {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "reader_url", "arguments": map[string]string{"path": path}}})
		req := httptest.NewRequest("POST", "http://brain.huynle.com/mcp", strings.NewReader(string(body)))
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("Authorization", "Bearer token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if path == "missing" {
			if !strings.Contains(w.Body.String(), "404") || !strings.Contains(w.Body.String(), `"isError":true`) {
				t.Fatal(w.Body.String())
			}
		} else if !strings.Contains(w.Body.String(), "https://brain.huynle.com/read.html?") {
			t.Fatal(w.Body.String())
		}
	}
	if shared.readerBaseURL != "" {
		t.Fatal("request origin escaped into shared client")
	}
}
