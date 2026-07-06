package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

func TestOpenRouterAttachmentExtractorDisabledReturnsSkipped(t *testing.T) {
	extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
		Enabled: false,
	})

	resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
		AttachmentID: "att_1",
		ContentType:  "image/png",
		Content:      []byte("png bytes"),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if resp.Status != types.AttachmentExtractionStatusSkipped {
		t.Fatalf("status = %q, want %q", resp.Status, types.AttachmentExtractionStatusSkipped)
	}
	if !strings.Contains(resp.Error, "disabled") {
		t.Fatalf("error = %q, want disabled reason", resp.Error)
	}
}

func TestOpenRouterAttachmentExtractorMapsJSONContentSummaryUsageAndMetadata(t *testing.T) {
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"content":"{\"text\":\"OCR text from image\",\"summary\":\"Short image summary\"}"}}],
			"model":"provider/model-v2",
			"usage":{"prompt_tokens":12,"completion_tokens":34,"total_tokens":46}
		}`))
	}))
	defer server.Close()

	extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
		Enabled:   true,
		BaseURL:   server.URL,
		APIKeyEnv: "BRAIN_TEST_OPENROUTER_KEY",
		Model:     "configured-model",
	})

	resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
		AttachmentID: "att_img",
		Filename:     "diagram.png",
		ContentType:  "image/png",
		Size:         7,
		Content:      []byte("imgdata"),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if resp.Status != types.AttachmentExtractionStatusReady {
		t.Fatalf("status = %q, want %q", resp.Status, types.AttachmentExtractionStatusReady)
	}
	if resp.Text != "OCR text from image" {
		t.Fatalf("text = %q, want JSON text field", resp.Text)
	}
	if resp.Summary != "Short image summary" {
		t.Fatalf("summary = %q, want JSON summary field", resp.Summary)
	}
	if resp.Model != "provider/model-v2" {
		t.Fatalf("model = %q, want response model", resp.Model)
	}

	wantMetadata := map[string]string{
		"provider":          "openrouter",
		"model_configured":  "configured-model",
		"model_response":    "provider/model-v2",
		"filename":          "diagram.png",
		"content_type":      "image/png",
		"size_bytes":        "7",
		"prompt_tokens":     "12",
		"completion_tokens": "34",
		"total_tokens":      "46",
	}
	for key, want := range wantMetadata {
		if resp.Metadata[key] != want {
			t.Fatalf("metadata[%q] = %q, want %q (metadata=%v)", key, resp.Metadata[key], want, resp.Metadata)
		}
	}
	if _, ok := resp.Metadata["raw_bytes"]; ok {
		t.Fatalf("metadata must not include raw bytes: %v", resp.Metadata)
	}
}

func TestOpenRouterAttachmentExtractorKeepsPlainTextContentCompatible(t *testing.T) {
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  plain derived text  "}}],"model":"test-model"}`))
	}))
	defer server.Close()

	extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
		Enabled:   true,
		BaseURL:   server.URL,
		APIKeyEnv: "BRAIN_TEST_OPENROUTER_KEY",
		Model:     "test-model",
	})

	resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
		AttachmentID: "att_img",
		ContentType:  "image/png",
		Content:      []byte("imgdata"),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if resp.Text != "plain derived text" {
		t.Fatalf("text = %q, want trimmed plain text", resp.Text)
	}
	if resp.Summary != "" {
		t.Fatalf("summary = %q, want empty for plain text", resp.Summary)
	}
}

func TestOpenRouterAttachmentExtractorTruncatesDerivedText(t *testing.T) {
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"abcdefghij"}}],"model":"test-model"}`))
	}))
	defer server.Close()

	extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
		Enabled:             true,
		BaseURL:             server.URL,
		APIKeyEnv:           "BRAIN_TEST_OPENROUTER_KEY",
		Model:               "test-model",
		MaxDerivedTextChars: 4,
	})

	resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
		AttachmentID: "att_img",
		ContentType:  "image/png",
		Content:      []byte("imgdata"),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if resp.Text != "abcd" {
		t.Fatalf("text = %q, want truncated text", resp.Text)
	}
	if resp.Metadata["truncated"] != "true" {
		t.Fatalf("metadata[truncated] = %q, want true", resp.Metadata["truncated"])
	}
	if resp.Metadata["original_text_chars"] != strconv.Itoa(len("abcdefghij")) {
		t.Fatalf("metadata[original_text_chars] = %q, want 10", resp.Metadata["original_text_chars"])
	}
	if resp.Metadata["max_derived_text_chars"] != "4" {
		t.Fatalf("metadata[max_derived_text_chars] = %q, want 4", resp.Metadata["max_derived_text_chars"])
	}
}

func TestOpenRouterAttachmentExtractorFailureSemantics(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{name: "non 2xx", statusCode: http.StatusTooManyRequests, body: `{"error":"rate limited"}`, wantError: "status 429"},
		{name: "decode error", statusCode: http.StatusOK, body: `{not json`, wantError: "decode OpenRouter"},
		{name: "empty choices", statusCode: http.StatusOK, body: `{"choices":[]}`, wantError: "no content"},
		{name: "blank content", statusCode: http.StatusOK, body: `{"choices":[{"message":{"content":"   "}}]}`, wantError: "no content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
				Enabled:   true,
				BaseURL:   server.URL,
				APIKeyEnv: "BRAIN_TEST_OPENROUTER_KEY",
				Model:     "test-model",
			})

			resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
				AttachmentID: "att_img",
				ContentType:  "image/png",
				Content:      []byte("imgdata"),
			})
			if err != nil {
				t.Fatalf("Extract returned error: %v", err)
			}
			if resp.Status != types.AttachmentExtractionStatusFailed {
				t.Fatalf("status = %q, want %q", resp.Status, types.AttachmentExtractionStatusFailed)
			}
			if !strings.Contains(resp.Error, tt.wantError) {
				t.Fatalf("error = %q, want containing %q", resp.Error, tt.wantError)
			}
		})
	}
}

func TestOpenRouterAttachmentExtractorSizeLimitReturnsSkipped(t *testing.T) {
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
		Enabled:      true,
		BaseURL:      server.URL,
		APIKeyEnv:    "BRAIN_TEST_OPENROUTER_KEY",
		Model:        "test-model",
		MaxSizeBytes: 4,
	})

	resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
		AttachmentID: "att_img",
		ContentType:  "image/png",
		Content:      []byte("abcde"),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if resp.Status != types.AttachmentExtractionStatusSkipped {
		t.Fatalf("status = %q, want %q", resp.Status, types.AttachmentExtractionStatusSkipped)
	}
	if !strings.Contains(resp.Error, "exceeds extraction limit") {
		t.Fatalf("error = %q, want size limit reason", resp.Error)
	}
	if !strings.Contains(resp.Error, "attachment size 5") {
		t.Fatalf("error = %q, want actual content length in size limit reason", resp.Error)
	}
	if called {
		t.Fatal("server was called for oversized attachment; want local skip")
	}
}

func TestOpenRouterAttachmentExtractorMissingAPIKeyReturnsSkipped(t *testing.T) {
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "")
	extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
		Enabled:   true,
		APIKeyEnv: "BRAIN_TEST_OPENROUTER_KEY",
	})

	resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
		AttachmentID: "att_1",
		ContentType:  "image/png",
		Content:      []byte("png bytes"),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if resp.Status != types.AttachmentExtractionStatusSkipped {
		t.Fatalf("status = %q, want %q", resp.Status, types.AttachmentExtractionStatusSkipped)
	}
	if !strings.Contains(resp.Error, "API key") {
		t.Fatalf("error = %q, want API key reason", resp.Error)
	}
}

func TestOpenRouterAttachmentExtractorUnsupportedMIMEReturnsSkipped(t *testing.T) {
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")
	extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
		Enabled:            true,
		APIKeyEnv:          "BRAIN_TEST_OPENROUTER_KEY",
		SupportedMIMETypes: []string{"image/*", "application/pdf", "audio/*", "video/*"},
	})

	resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
		AttachmentID: "att_pdf",
		ContentType:  "application/pdf",
		Content:      []byte("%PDF"),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if resp.Status != types.AttachmentExtractionStatusSkipped {
		t.Fatalf("status = %q, want %q", resp.Status, types.AttachmentExtractionStatusSkipped)
	}
	if !strings.Contains(resp.Error, "unsupported") {
		t.Fatalf("error = %q, want unsupported reason", resp.Error)
	}
}

func TestOpenRouterAttachmentExtractorImageRequestShape(t *testing.T) {
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")

	var gotPath string
	var gotAuth string
	var gotContentType string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"derived text"}}],"model":"test-model"}`))
	}))
	defer server.Close()

	extractor := NewOpenRouterAttachmentExtractor(config.AttachmentExtractionConfig{
		Enabled:            true,
		Provider:           "openrouter",
		BaseURL:            server.URL,
		APIKeyEnv:          "BRAIN_TEST_OPENROUTER_KEY",
		Model:              "test-model",
		SupportedMIMETypes: []string{"image/*"},
	})

	resp, err := extractor.Extract(context.Background(), types.AttachmentExtractionRequest{
		AttachmentID: "att_img",
		Filename:     "pixel.png",
		ContentType:  "image/png",
		Content:      []byte("abc123"),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if resp.Status != types.AttachmentExtractionStatusReady {
		t.Fatalf("status = %q, want %q", resp.Status, types.AttachmentExtractionStatusReady)
	}

	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q, want bearer key", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", gotContentType)
	}
	if body["model"] != "test-model" {
		t.Fatalf("model = %#v, want test-model", body["model"])
	}

	messages := body["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	imagePart := content[1].(map[string]any)
	if imagePart["type"] != "image_url" {
		t.Fatalf("image part type = %#v, want image_url", imagePart["type"])
	}
	imageURL := imagePart["image_url"].(map[string]any)["url"].(string)
	if imageURL != "data:image/png;base64,YWJjMTIz" {
		t.Fatalf("image URL = %q, want data URL", imageURL)
	}
}
