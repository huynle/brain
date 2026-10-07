package sdkcontract_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/service"
)

// Deterministic provider fixture, not remote model-quality evidence. All Brain
// service, HTTP adapter, blob and derived-text persistence paths remain real.
func sdkExtractionFixture(t *testing.T) (service.AttachmentExtractor, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	t.Setenv("BRAIN_SDK_FIXTURE_PROVIDER_KEY", "fixture-provider-key")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-provider-key" {
			t.Errorf("unexpected provider request boundary")
			http.Error(w, "invalid fixture request", 400)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			t.Error(err)
			return
		}
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Content []struct {
					Type     string `json:"type"`
					ImageURL *struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &request); err != nil || request.Model != "fixture-model" {
			t.Errorf("invalid provider payload: %v", err)
			http.Error(w, "invalid fixture payload", 400)
			return
		}
		image := false
		for _, message := range request.Messages {
			for _, part := range message.Content {
				if part.Type == "image_url" && part.ImageURL != nil && strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,iVBORw0KGgo") {
					image = true
				}
			}
		}
		if !image {
			t.Error("provider payload missing encoded image")
			http.Error(w, "missing image", 400)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"fixture-model","choices":[{"message":{"content":"{\"text\":\"fixture extracted text\",\"summary\":\"fixture\"}"}}],"usage":{"total_tokens":7}}`))
	}))
	t.Cleanup(provider.Close)
	return service.NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{Enabled: true, BaseURL: provider.URL, APIKeyEnv: "BRAIN_SDK_FIXTURE_PROVIDER_KEY", Model: "fixture-model", TimeoutMs: 5000, MaxSizeBytes: 1 << 20, MaxDerivedTextChars: 1000}), &calls
}
