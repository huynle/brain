package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type SpeechTranscriber interface {
	Transcribe(context.Context, string) (string, error)
}
type openRouterTranscriber struct {
	baseURL, key, model string
	client              *http.Client
}

func newTranscriber(opts SpeechOptions) SpeechTranscriber {
	if !opts.Enabled || (opts.Provider != "" && opts.Provider != "openrouter") {
		return nil
	}
	return &openRouterTranscriber{strings.TrimRight(firstNonEmptyString(opts.BaseURL, "https://openrouter.ai/api/v1"), "/"), os.Getenv(firstNonEmptyString(opts.APIKeyEnv, "OPENROUTER_API_KEY")), firstNonEmptyString(os.Getenv("BRAIN_ASSISTANT_TRANSCRIPTION_MODEL"), "openai/whisper-large-v3-turbo"), &http.Client{Timeout: 60 * time.Second}}
}
func (s *openRouterTranscriber) Transcribe(ctx context.Context, data string) (string, error) {
	if s.key == "" {
		return "", fmt.Errorf("transcription provider is not configured")
	}
	body, _ := json.Marshal(map[string]any{"model": s.model, "input_audio": map[string]string{"data": data, "format": "wav"}})
	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/audio/transcriptions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcription connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("transcription provider returned HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return "", fmt.Errorf("invalid transcription response")
	}
	var out struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return "", fmt.Errorf("invalid transcription response")
	}
	return strings.TrimSpace(out.Text), nil
}
func (h *Handler) HandleAssistantTranscription(w http.ResponseWriter, r *http.Request) {
	if h.assistant == nil || !h.assistant.enabled || h.assistant.transcriber == nil {
		WriteError(w, 503, "Transcription unavailable", "Assistant speech is not enabled")
		return
	}
	var in struct {
		Audio string `json:"audio"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 3*1024*1024))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		WriteError(w, 400, "Invalid audio", "Provide one audio object")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(in.Audio)
	if err != nil || len(raw) < 44 || len(raw) > 2*1024*1024 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		WriteError(w, 400, "Invalid audio", "Provide a WAV segment up to 2 MiB")
		return
	}
	text, err := h.assistant.transcriber.Transcribe(r.Context(), in.Audio)
	if err != nil {
		WriteError(w, 502, "Transcription failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"text": text})
}
