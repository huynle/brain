package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantSpeechProvider(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		kind, body string
		wantError  bool
	}{
		{"audio", 200, "audio/mpeg", "ID3test", false},
		{"provider error", 401, "application/json", "secret-provider-body", true},
		{"wrong media", 200, "text/html", "not audio", true},
		{"empty", 200, "audio/mpeg", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/audio/speech" || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("incorrect provider request")
				}
				var body map[string]string
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["input"] != "Hello" || body["response_format"] != "mp3" {
					t.Error("incorrect speech payload")
				}
				w.Header().Set("Content-Type", tc.kind)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			s := &openRouterSpeech{baseURL: server.URL, key: "fixture", model: "test-model", voice: "test-voice", client: server.Client()}
			data, kind, err := s.Synthesize(context.Background(), "Hello")
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-provider-body") {
				t.Fatal("upstream body leaked")
			}
			if err == nil && (string(data) != tc.body || kind != "audio/mpeg") {
				t.Fatal("incorrect audio")
			}
		})
	}
}

type fixtureSpeech struct{ calls int }

func (s *fixtureSpeech) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	s.calls++
	return []byte("ID3test"), "audio/mpeg", nil
}
func TestAssistantSpeechValidation(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"text":"Hello"}`, 200}, {`{"text":" "}`, 400}, {`{"text":"hello"} {}`, 400}, {`not JSON`, 400},
		{`{"text":"` + strings.Repeat("x", 6001) + `"}`, 400},
	} {
		s := &fixtureSpeech{}
		h := &Handler{assistant: &AssistantService{enabled: true, speech: s}}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/speech", strings.NewReader(tc.body))
		h.HandleAssistantSpeech(w, r)
		if w.Code != tc.status {
			t.Fatalf("status %d, want %d", w.Code, tc.status)
		}
		if tc.status == 400 && s.calls != 0 {
			t.Fatal("invalid text billed to provider")
		}
		if tc.status == 200 && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("audio must not be cached")
		}
	}
	h := &Handler{assistant: &AssistantService{enabled: false, speech: &fixtureSpeech{}}}
	w := httptest.NewRecorder()
	h.HandleAssistantSpeech(w, httptest.NewRequest("POST", "/speech", strings.NewReader(`{"text":"Hello"}`)))
	if w.Code != 503 {
		t.Fatal("disabled speech accepted")
	}
}
func TestAssistantSpeechCancellation(t *testing.T) {
	s := &openRouterSpeech{baseURL: "http://127.0.0.1:1", key: "fixture", client: http.DefaultClient}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.Synthesize(ctx, "hello"); err == nil {
		t.Fatal("canceled request succeeded")
	}
}
