package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

const (
	defaultAttachmentExtractionProvider  = "openrouter"
	defaultAttachmentExtractionBaseURL   = "https://openrouter.ai/api/v1"
	defaultAttachmentExtractionAPIKeyEnv = "OPENROUTER_API_KEY"
	defaultAttachmentExtractionModel     = "google/gemini-2.5-flash"
	defaultAttachmentExtractionTimeout   = 60 * time.Second
)

// OpenRouterAttachmentExtractor extracts text using an OpenRouter-compatible
// multimodal chat/completions endpoint.
type OpenRouterAttachmentExtractor struct {
	enabled             bool
	disabledReason      string
	provider            string
	baseURL             string
	apiKey              string
	model               string
	timeout             time.Duration
	maxSizeBytes        int64
	maxDerivedTextChars int
	supportedMIMETypes  []string
	client              *http.Client
}

type openRouterChatRequest struct {
	Model    string                  `json:"model"`
	Messages []openRouterChatMessage `json:"messages"`
}

type openRouterChatMessage struct {
	Role    string                  `json:"role"`
	Content []openRouterContentPart `json:"content"`
}

type openRouterContentPart struct {
	Type     string                    `json:"type"`
	Text     string                    `json:"text,omitempty"`
	ImageURL *openRouterImageURLObject `json:"image_url,omitempty"`
}

type openRouterImageURLObject struct {
	URL string `json:"url"`
}

type openRouterChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openRouterStructuredContent struct {
	Text        string `json:"text"`
	DerivedText string `json:"derived_text"`
	Summary     string `json:"summary"`
}

// NewOpenRouterAttachmentExtractor returns a usable extractor for all config
// states. Disabled or missing-key configurations return skipped results from
// Extract rather than panicking or forcing callers to special-case nil.
func NewOpenRouterAttachmentExtractor(cfg config.AttachmentExtractionConfig) AttachmentExtractor {
	provider := cfg.Provider
	if provider == "" {
		provider = defaultAttachmentExtractionProvider
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultAttachmentExtractionBaseURL
	}

	apiKeyEnv := cfg.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = defaultAttachmentExtractionAPIKeyEnv
	}

	model := cfg.Model
	if model == "" {
		model = defaultAttachmentExtractionModel
	}

	timeout := defaultAttachmentExtractionTimeout
	if cfg.TimeoutMs > 0 {
		timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}

	supported := cfg.SupportedMIMETypes
	if len(supported) == 0 {
		supported = []string{"image/*"}
	}

	extractor := &OpenRouterAttachmentExtractor{
		enabled:             cfg.Enabled,
		provider:            provider,
		baseURL:             baseURL,
		model:               model,
		timeout:             timeout,
		maxSizeBytes:        cfg.MaxSizeBytes,
		maxDerivedTextChars: cfg.MaxDerivedTextChars,
		supportedMIMETypes:  supported,
		client:              &http.Client{Timeout: timeout},
	}

	if !cfg.Enabled {
		extractor.disabledReason = "attachment extraction disabled"
		return extractor
	}

	extractor.apiKey = os.Getenv(apiKeyEnv)
	if extractor.apiKey == "" {
		extractor.enabled = false
		extractor.disabledReason = fmt.Sprintf("attachment extraction API key not found in environment variable %s", apiKeyEnv)
	}

	return extractor
}

// AttachmentExtractionAvailable reports whether the OpenRouter extractor can
// process attachments without first reading attachment content.
func (e *OpenRouterAttachmentExtractor) AttachmentExtractionAvailable() (bool, string) {
	if e == nil {
		return false, "attachment extraction disabled"
	}
	if e.enabled {
		return true, ""
	}
	reason := strings.TrimSpace(e.disabledReason)
	if reason == "" {
		reason = "attachment extraction disabled"
	}
	return false, reason
}

// Extract derives text from a supported image attachment. Non-image media are
// intentionally skipped in this phase even when present in configuration.
func (e *OpenRouterAttachmentExtractor) Extract(ctx context.Context, req types.AttachmentExtractionRequest) (types.AttachmentExtractionResponse, error) {
	started := time.Now()
	baseResp := types.AttachmentExtractionResponse{
		AttachmentID: req.AttachmentID,
		Provider:     e.provider,
		Model:        e.model,
		ContentType:  req.ContentType,
		Metadata:     e.baseMetadata(req),
	}

	if !e.enabled {
		return e.skipped(baseResp, e.disabledReason, started), nil
	}

	mediaType := normalizeMediaType(req.ContentType)
	if !e.supportsImageMIME(mediaType) {
		return e.skipped(baseResp, fmt.Sprintf("unsupported attachment MIME type %q for OpenRouter image extraction", req.ContentType), started), nil
	}

	size := effectiveAttachmentSize(req)
	if e.maxSizeBytes > 0 && size > e.maxSizeBytes {
		return e.skipped(baseResp, fmt.Sprintf("attachment size %d exceeds extraction limit %d", size, e.maxSizeBytes), started), nil
	}

	body, err := json.Marshal(openRouterChatRequest{
		Model: e.model,
		Messages: []openRouterChatMessage{{
			Role: "user",
			Content: []openRouterContentPart{
				{Type: "text", Text: imageExtractionPrompt(req)},
				{Type: "image_url", ImageURL: &openRouterImageURLObject{URL: dataURL(mediaType, req.Content)}},
			},
		}},
	})
	if err != nil {
		return types.AttachmentExtractionResponse{}, fmt.Errorf("marshal OpenRouter attachment extraction request: %w", err)
	}

	url := e.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return types.AttachmentExtractionResponse{}, fmt.Errorf("create OpenRouter attachment extraction request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)

	slog.Debug("attachment extraction API request",
		"provider", e.provider,
		"model", e.model,
		"content_type", mediaType,
		"size", len(req.Content),
	)

	httpResp, err := e.client.Do(httpReq)
	if err != nil {
		return types.AttachmentExtractionResponse{}, fmt.Errorf("send OpenRouter attachment extraction request: %w", err)
	}
	defer httpResp.Body.Close()

	slog.Debug("attachment extraction API response",
		"provider", e.provider,
		"model", e.model,
		"status", httpResp.StatusCode,
	)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		baseResp.Status = types.AttachmentExtractionStatusFailed
		baseResp.Error = fmt.Sprintf("OpenRouter attachment extraction returned status %d", httpResp.StatusCode)
		baseResp.DurationMs = time.Since(started).Milliseconds()
		return baseResp, nil
	}

	var parsed openRouterChatResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&parsed); err != nil {
		baseResp.Status = types.AttachmentExtractionStatusFailed
		baseResp.Error = fmt.Sprintf("decode OpenRouter attachment extraction response: %v", err)
		baseResp.DurationMs = time.Since(started).Milliseconds()
		return baseResp, nil
	}

	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		baseResp.Status = types.AttachmentExtractionStatusFailed
		baseResp.Error = "OpenRouter attachment extraction returned no content"
		baseResp.DurationMs = time.Since(started).Milliseconds()
		return baseResp, nil
	}

	derivedText, summary := parseOpenRouterDerivedContent(parsed.Choices[0].Message.Content)
	if strings.TrimSpace(derivedText) == "" {
		baseResp.Status = types.AttachmentExtractionStatusFailed
		baseResp.Error = "OpenRouter attachment extraction returned no content"
		baseResp.DurationMs = time.Since(started).Milliseconds()
		return baseResp, nil
	}

	baseResp.Status = types.AttachmentExtractionStatusReady
	baseResp.Text = e.truncateDerivedText(strings.TrimSpace(derivedText), baseResp.Metadata)
	baseResp.Summary = strings.TrimSpace(summary)
	baseResp.DurationMs = time.Since(started).Milliseconds()
	if parsed.Model != "" {
		baseResp.Model = parsed.Model
		baseResp.Metadata["model_response"] = parsed.Model
	}
	e.addUsageMetadata(baseResp.Metadata, parsed)
	return baseResp, nil
}

func (e *OpenRouterAttachmentExtractor) skipped(resp types.AttachmentExtractionResponse, reason string, started time.Time) types.AttachmentExtractionResponse {
	resp.Status = types.AttachmentExtractionStatusSkipped
	resp.Error = reason
	resp.DurationMs = time.Since(started).Milliseconds()
	return resp
}

func (e *OpenRouterAttachmentExtractor) baseMetadata(req types.AttachmentExtractionRequest) map[string]string {
	metadata := map[string]string{
		"provider":         e.provider,
		"model_configured": e.model,
		"content_type":     req.ContentType,
	}
	if strings.TrimSpace(req.Filename) != "" {
		metadata["filename"] = strings.TrimSpace(req.Filename)
	}
	size := effectiveAttachmentSize(req)
	if size > 0 {
		metadata["size_bytes"] = fmt.Sprintf("%d", size)
	}
	return metadata
}

func effectiveAttachmentSize(req types.AttachmentExtractionRequest) int64 {
	if req.Size > 0 {
		return req.Size
	}
	return int64(len(req.Content))
}

func (e *OpenRouterAttachmentExtractor) addUsageMetadata(metadata map[string]string, parsed openRouterChatResponse) {
	if parsed.Usage.PromptTokens > 0 {
		metadata["prompt_tokens"] = fmt.Sprintf("%d", parsed.Usage.PromptTokens)
	}
	if parsed.Usage.CompletionTokens > 0 {
		metadata["completion_tokens"] = fmt.Sprintf("%d", parsed.Usage.CompletionTokens)
	}
	if parsed.Usage.TotalTokens > 0 {
		metadata["total_tokens"] = fmt.Sprintf("%d", parsed.Usage.TotalTokens)
	}
}

func (e *OpenRouterAttachmentExtractor) truncateDerivedText(text string, metadata map[string]string) string {
	if e.maxDerivedTextChars <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= e.maxDerivedTextChars {
		return text
	}
	metadata["truncated"] = "true"
	metadata["original_text_chars"] = fmt.Sprintf("%d", len(runes))
	metadata["max_derived_text_chars"] = fmt.Sprintf("%d", e.maxDerivedTextChars)
	return string(runes[:e.maxDerivedTextChars])
}

func parseOpenRouterDerivedContent(content string) (string, string) {
	trimmed := strings.TrimSpace(content)
	var structured openRouterStructuredContent
	if err := json.Unmarshal([]byte(trimmed), &structured); err == nil {
		text := structured.Text
		if strings.TrimSpace(text) == "" {
			text = structured.DerivedText
		}
		if strings.TrimSpace(text) != "" || strings.TrimSpace(structured.Summary) != "" {
			return strings.TrimSpace(text), strings.TrimSpace(structured.Summary)
		}
	}
	return trimmed, ""
}

func (e *OpenRouterAttachmentExtractor) supportsImageMIME(contentType string) bool {
	if !strings.HasPrefix(contentType, "image/") {
		return false
	}
	for _, supported := range e.supportedMIMETypes {
		supported = strings.ToLower(strings.TrimSpace(supported))
		if supported == "image/*" || supported == contentType {
			return true
		}
	}
	return false
}

func normalizeMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return strings.ToLower(strings.TrimSpace(contentType))
	}
	return strings.ToLower(mediaType)
}

func dataURL(contentType string, content []byte) string {
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(content)
}

func imageExtractionPrompt(req types.AttachmentExtractionRequest) string {
	if strings.TrimSpace(req.Filename) == "" {
		return "Extract searchable text from this image. Return concise derived text only."
	}
	return fmt.Sprintf("Extract searchable text from image %q. Return concise derived text only.", req.Filename)
}
