package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
)

var voiceAttemptID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)
var voiceDiagnosticEvents = map[string]bool{"start_requested": true, "started": true, "audio_start": true, "sound_start": true, "speech_start": true, "result": true, "error": true, "ended": true, "waiting": true, "stopped": true, "start_failed": true}
var voiceDiagnosticErrors = map[string]bool{"": true, "no-speech": true, "aborted": true, "audio-capture": true, "network": true, "not-allowed": true, "service-not-allowed": true, "bad-grammar": true, "language-not-supported": true, "unknown": true}

// Deliberately excludes transcript, audio, device names and arbitrary error text.
func (h *Handler) HandleAssistantVoiceDiagnostics(w http.ResponseWriter, r *http.Request) {
	var event struct {
		Attempt   string `json:"attempt"`
		Event     string `json:"event"`
		Error     string `json:"error"`
		ElapsedMS int64  `json:"elapsed_ms"`
		Results   int    `json:"results"`
		Android   bool   `json:"android"`
		HandsFree bool   `json:"hands_free"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	d.DisallowUnknownFields()
	if d.Decode(&event) != nil || d.Decode(new(any)) != io.EOF || !voiceAttemptID.MatchString(event.Attempt) || !voiceDiagnosticEvents[event.Event] || !voiceDiagnosticErrors[event.Error] || event.ElapsedMS < 0 || event.ElapsedMS > 86400000 || event.Results < 0 || event.Results > 100000 {
		http.Error(w, "invalid voice diagnostic", http.StatusBadRequest)
		return
	}
	slog.Info("assistant voice diagnostic", "attempt", event.Attempt, "event", event.Event, "error", event.Error, "elapsed_ms", event.ElapsedMS, "results", event.Results, "android", event.Android, "hands_free", event.HandsFree)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
