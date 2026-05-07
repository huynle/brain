package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ollamaProvider       = "ollama"
	defaultOllamaBaseURL = "http://localhost:11434"
	defaultOllamaTimeout = 30 * time.Second
)

// OllamaConfig configures the Ollama embedding provider.
type OllamaConfig struct {
	BaseURL    string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// OllamaEmbedder calls Ollama's /api/embed endpoint.
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaEmbedder creates an Ollama embedding provider.
func NewOllamaEmbedder(cfg OllamaConfig) (*OllamaEmbedder, error) {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("ollama model is required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid ollama base URL %q", baseURL)
	}

	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultOllamaTimeout
		}
		client = &http.Client{Timeout: timeout}
	}

	return &OllamaEmbedder{
		baseURL: strings.TrimRight(parsed.String(), "/"),
		model:   model,
		client:  client,
	}, nil
}

// Embed returns embeddings for texts in the same order as requested.
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(ollamaEmbedRequest{
		Model: e.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("encode ollama embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ollama embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, &ProviderError{Provider: ollamaProvider, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		providerErr := &ProviderError{Provider: ollamaProvider, StatusCode: resp.StatusCode}
		if data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096)); readErr == nil {
			providerErr.Body = strings.TrimSpace(string(data))
		}
		return nil, providerErr
	}

	var decoded ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, &ProviderError{Provider: ollamaProvider, Err: fmt.Errorf("decode response: %w", err)}
	}
	if len(decoded.Embeddings) != len(texts) {
		return nil, &ProviderError{Provider: ollamaProvider, Err: fmt.Errorf("response embeddings count %d does not match request count %d", len(decoded.Embeddings), len(texts))}
	}
	for i, embedding := range decoded.Embeddings {
		if len(embedding) == 0 {
			return nil, &ProviderError{Provider: ollamaProvider, Err: fmt.Errorf("empty embedding at index %d", i)}
		}
	}

	return decoded.Embeddings, nil
}

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}
