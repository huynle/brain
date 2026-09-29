package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// SpeechSynthesizer isolates model-specific transports from the chat API.
// A Breeze implementation can use its multipart/PCM API behind this interface.
type SpeechSynthesizer interface {
	Synthesize(context.Context, string) ([]byte, string, error)
}
type SpeechOptions struct {
	Enabled                                    bool
	Provider, BaseURL, APIKeyEnv, Model, Voice string
}
type openRouterSpeech struct {
	baseURL, key, model, voice string
	client                     *http.Client
}

func newSpeechProvider(opts SpeechOptions) SpeechSynthesizer {
	if !opts.Enabled || (opts.Provider != "" && opts.Provider != "openrouter") {
		return nil
	}
	return &openRouterSpeech{
		baseURL: strings.TrimRight(firstNonEmptyString(opts.BaseURL, "https://openrouter.ai/api/v1"), "/"),
		key:     os.Getenv(firstNonEmptyString(opts.APIKeyEnv, "OPENROUTER_API_KEY")),
		model:   firstNonEmptyString(opts.Model, "hexgrad/kokoro-82m"),
		voice:   firstNonEmptyString(opts.Voice, "af_heart"),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}
func (s *openRouterSpeech) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	if s.key == "" {
		return nil, "", fmt.Errorf("speech API key is not configured")
	}
	body, _ := json.Marshal(map[string]string{"model": s.model, "input": text, "voice": s.voice, "response_format": "mp3"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.key)
	res, err := s.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("speech provider connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("speech provider returned HTTP %d", res.StatusCode)
	}
	contentType := strings.Split(res.Header.Get("Content-Type"), ";")[0]
	if contentType != "audio/mpeg" && contentType != "audio/mp3" {
		return nil, "", fmt.Errorf("speech provider returned an unsupported audio format")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024+1))
	if err != nil || len(data) == 0 || len(data) > 8*1024*1024 {
		return nil, "", fmt.Errorf("speech provider returned invalid audio")
	}
	return data, "audio/mpeg", nil
}
func (h *Handler) HandleAssistantSpeech(w http.ResponseWriter, r *http.Request) {
	if h.assistant == nil || !h.assistant.enabled || h.assistant.speech == nil {
		WriteError(w, http.StatusServiceUnavailable, "Speech unavailable", "Assistant speech is not enabled")
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		WriteError(w, 400, "Invalid request", "Provide speech text")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		WriteError(w, 400, "Invalid request", "Provide one JSON object")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || utf8.RuneCountInString(req.Text) > 6000 {
		WriteError(w, 400, "Invalid text", "Speech text must contain 1 to 6000 characters")
		return
	}
	data, kind, err := h.assistant.speech.Synthesize(r.Context(), req.Text)
	if err != nil {
		WriteError(w, 502, "Speech failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
