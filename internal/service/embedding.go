package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

// EmbeddingClient is the interface for generating text embeddings.
type EmbeddingClient interface {
	// Embed generates embeddings for the given input texts.
	// Returns a slice of embeddings (one per input) where each embedding is a float32 slice.
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// AiFactoryEmbeddingClient implements EmbeddingClient using the AiFactory API.
type AiFactoryEmbeddingClient struct {
	baseURL   string
	apiKey    string
	model     string
	batchSize int
	timeout   time.Duration
	client    *http.Client
}

// embeddingRequest represents the AiFactory embeddings API request body.
type embeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// embeddingResponse represents the AiFactory embeddings API response body.
type embeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// NewAiFactoryEmbeddingClient creates a new OpenAI-compatible embedding client using the provided config.
// If config values are not set, it uses sensible defaults:
// - baseURL: https://openrouter.ai/api/v1
// - apiKeyEnv: OPENROUTER_API_KEY
// - model: text-embedding-3-small
// - batchSize: 32
// - timeout: 30s
func NewAiFactoryEmbeddingClient(cfg config.EmbeddingConfig) (*AiFactoryEmbeddingClient, error) {
	// Apply defaults
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}

	apiKeyEnv := cfg.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = "OPENROUTER_API_KEY"
	}

	model := cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}

	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 30000 // 30 seconds
	}

	// Read API key from environment
	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("API key not found in environment variable %s", apiKeyEnv)
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond

	return &AiFactoryEmbeddingClient{
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     model,
		batchSize: batchSize,
		timeout:   timeout,
		client:    &http.Client{Timeout: timeout},
	}, nil
}

// Embed generates embeddings for the given inputs by calling the AiFactory /embeddings endpoint.
// It batches requests according to the configured batch size.
func (c *AiFactoryEmbeddingClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}

	var allEmbeddings [][]float32

	// Process in batches
	for i := 0; i < len(inputs); i += c.batchSize {
		end := i + c.batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[i:end]

		embeddings, err := c.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("failed to embed batch starting at index %d: %w", i, err)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

// embedBatch sends a single batch request to the AiFactory API.
func (c *AiFactoryEmbeddingClient) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	reqBody := embeddingRequest{
		Input: inputs,
		Model: c.model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// Debug-level trace for outbound embedding call (no sensitive payload)
	slog.Debug("embedding API request",
		"url", url,
		"model", c.model,
		"inputs", len(inputs),
	)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	slog.Debug("embedding API response",
		"model", c.model,
		"status", resp.StatusCode,
	)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var respBody embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract embeddings in order
	embeddings := make([][]float32, len(respBody.Data))
	for _, item := range respBody.Data {
		if item.Index < 0 || item.Index >= len(embeddings) {
			return nil, fmt.Errorf("invalid embedding index %d in response", item.Index)
		}
		embeddings[item.Index] = item.Embedding
	}

	return embeddings, nil
}
