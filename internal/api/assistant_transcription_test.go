package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranscriptionProvider(t *testing.T) {
	for _, code := range []int{200, 502} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer fixture" {
				t.Error("bad provider request")
			}
			var body struct {
				Model string            `json:"model"`
				Input map[string]string `json:"input_audio"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Model != "test" || body.Input["format"] != "wav" || body.Input["data"] != "fixture" {
				t.Error("bad transcription body")
			}
			w.WriteHeader(code)
			if code == 200 {
				w.Write([]byte(`{"text":" Hello "}`))
			} else {
				w.Write([]byte("private provider details"))
			}
		}))
		s := &openRouterTranscriber{server.URL, "fixture", "test", server.Client()}
		text, err := s.Transcribe(context.Background(), "fixture")
		if code == 200 && (err != nil || text != "Hello") {
			t.Fatalf("unexpected success %q %v", text, err)
		}
		if code != 200 && (err == nil || strings.Contains(err.Error(), "private")) {
			t.Fatal("provider errors must be sanitized")
		}
		server.Close()
	}
}
func TestTranscriptionRejectsInvalidAudio(t *testing.T) {
	h := &Handler{assistant: &AssistantService{enabled: true, transcriber: &openRouterTranscriber{}}}
	for _, body := range []string{`{}`, `{"audio":"not base64"}`, `{"audio":"","text":"private"}`} {
		w := httptest.NewRecorder()
		h.HandleAssistantTranscription(w, httptest.NewRequest("POST", "/transcribe", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("status %d", w.Code)
		}
	}
}
