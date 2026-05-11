package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

func TestNewAiFactoryEmbeddingClient(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.EmbeddingConfig
		envKey     string
		envValue   string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "valid config with defaults",
			cfg: config.EmbeddingConfig{
				APIKeyEnv: "TEST_API_KEY",
			},
			envKey:   "TEST_API_KEY",
			envValue: "test-key-123",
			wantErr:  false,
		},
		{
			name: "custom config",
			cfg: config.EmbeddingConfig{
				BaseURL:   "https://custom.api.com/v2",
				APIKeyEnv: "CUSTOM_KEY",
				Model:     "custom-model",
				BatchSize: 64,
				TimeoutMs: 60000,
			},
			envKey:   "CUSTOM_KEY",
			envValue: "custom-key-456",
			wantErr:  false,
		},
		{
			name: "missing API key",
			cfg: config.EmbeddingConfig{
				APIKeyEnv: "MISSING_KEY",
			},
			envKey:     "",
			envValue:   "",
			wantErr:    true,
			wantErrMsg: "API key not found",
		},
		{
			name:     "default OpenRouter API key env var",
			cfg:      config.EmbeddingConfig{},
			envKey:   "OPENROUTER_API_KEY",
			envValue: "default-key",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.envKey != "" {
				os.Setenv(tt.envKey, tt.envValue)
				defer os.Unsetenv(tt.envKey)
			}

			client, err := NewAiFactoryEmbeddingClient(tt.cfg)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErrMsg)
				} else if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("expected error containing %q, got %q", tt.wantErrMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if client == nil {
				t.Fatal("expected client, got nil")
			}

			// Verify defaults were applied
			if tt.cfg.BaseURL == "" && client.baseURL != "https://openrouter.ai/api/v1" {
				t.Errorf("expected default baseURL, got %q", client.baseURL)
			}
			if tt.cfg.Model == "" && client.model != "text-embedding-3-small" {
				t.Errorf("expected default model, got %q", client.model)
			}
			if tt.cfg.BatchSize == 0 && client.batchSize != 32 {
				t.Errorf("expected default batchSize 32, got %d", client.batchSize)
			}
			if tt.cfg.TimeoutMs == 0 && client.timeout != 30*time.Second {
				t.Errorf("expected default timeout 30s, got %v", client.timeout)
			}

			// Verify custom values
			if tt.cfg.BaseURL != "" && client.baseURL != tt.cfg.BaseURL {
				t.Errorf("expected baseURL %q, got %q", tt.cfg.BaseURL, client.baseURL)
			}
			if tt.cfg.Model != "" && client.model != tt.cfg.Model {
				t.Errorf("expected model %q, got %q", tt.cfg.Model, client.model)
			}
			if tt.cfg.BatchSize > 0 && client.batchSize != tt.cfg.BatchSize {
				t.Errorf("expected batchSize %d, got %d", tt.cfg.BatchSize, client.batchSize)
			}
		})
	}
}

func TestAiFactoryEmbeddingClient_Embed_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Errorf("expected /embeddings, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("expected Authorization Bearer header, got %s", r.Header.Get("Authorization"))
		}

		// Decode request
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		// Generate mock embeddings
		resp := embeddingResponse{
			Object: "list",
			Model:  req.Model,
			Data: make([]struct {
				Object    string    `json:"object"`
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}, len(req.Input)),
		}

		for i := range req.Input {
			resp.Data[i].Object = "embedding"
			resp.Data[i].Index = i
			// Generate simple mock embedding based on string length
			resp.Data[i].Embedding = []float32{float32(len(req.Input[i])), 0.5, 0.8}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Set up test client
	os.Setenv("TEST_EMBED_KEY", "test-key")
	defer os.Unsetenv("TEST_EMBED_KEY")

	client, err := NewAiFactoryEmbeddingClient(config.EmbeddingConfig{
		BaseURL:   server.URL,
		APIKeyEnv: "TEST_EMBED_KEY",
		Model:     "test-model",
		BatchSize: 32,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	tests := []struct {
		name   string
		inputs []string
	}{
		{
			name:   "single input",
			inputs: []string{"hello"},
		},
		{
			name:   "multiple inputs",
			inputs: []string{"hello", "world", "test"},
		},
		{
			name:   "empty input list",
			inputs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			embeddings, err := client.Embed(ctx, tt.inputs)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(embeddings) != len(tt.inputs) {
				t.Errorf("expected %d embeddings, got %d", len(tt.inputs), len(embeddings))
			}

			for i, emb := range embeddings {
				if len(emb) != 3 {
					t.Errorf("expected embedding %d to have 3 dimensions, got %d", i, len(emb))
				}
				if emb[0] != float32(len(tt.inputs[i])) {
					t.Errorf("expected embedding[%d][0] = %f, got %f", i, float32(len(tt.inputs[i])), emb[0])
				}
			}
		})
	}
}

func TestAiFactoryEmbeddingClient_Embed_Batching(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		resp := embeddingResponse{
			Object: "list",
			Model:  req.Model,
			Data: make([]struct {
				Object    string    `json:"object"`
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}, len(req.Input)),
		}

		for i := range req.Input {
			resp.Data[i].Object = "embedding"
			resp.Data[i].Index = i
			resp.Data[i].Embedding = []float32{1.0, 2.0, 3.0}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("TEST_BATCH_KEY", "test-key")
	defer os.Unsetenv("TEST_BATCH_KEY")

	// Create client with small batch size
	client, err := NewAiFactoryEmbeddingClient(config.EmbeddingConfig{
		BaseURL:   server.URL,
		APIKeyEnv: "TEST_BATCH_KEY",
		BatchSize: 5,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test with 12 inputs (should result in 3 batches of size 5, 5, 2)
	inputs := make([]string, 12)
	for i := range inputs {
		inputs[i] = fmt.Sprintf("text-%d", i)
	}

	ctx := context.Background()
	embeddings, err := client.Embed(ctx, inputs)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(embeddings) != 12 {
		t.Errorf("expected 12 embeddings, got %d", len(embeddings))
	}

	if requestCount != 3 {
		t.Errorf("expected 3 batch requests, got %d", requestCount)
	}
}

func TestAiFactoryEmbeddingClient_Embed_Errors(t *testing.T) {
	tests := []struct {
		name       string
		serverFunc func(w http.ResponseWriter, r *http.Request)
		wantErrMsg string
	}{
		{
			name: "API error response",
			serverFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error": "invalid model"}`))
			},
			wantErrMsg: "API returned status 400",
		},
		{
			name: "invalid JSON response",
			serverFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`invalid json`))
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "unauthorized",
			serverFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error": "invalid API key"}`))
			},
			wantErrMsg: "API returned status 401",
		},
		{
			name: "server error",
			serverFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error": "internal server error"}`))
			},
			wantErrMsg: "API returned status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverFunc))
			defer server.Close()

			os.Setenv("TEST_ERROR_KEY", "test-key")
			defer os.Unsetenv("TEST_ERROR_KEY")

			client, err := NewAiFactoryEmbeddingClient(config.EmbeddingConfig{
				BaseURL:   server.URL,
				APIKeyEnv: "TEST_ERROR_KEY",
				TimeoutMs: 5000,
			})
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			ctx := context.Background()
			_, err = client.Embed(ctx, []string{"test"})

			if err == nil {
				t.Fatalf("expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("expected error containing %q, got %q", tt.wantErrMsg, err.Error())
			}
		})
	}
}

func TestAiFactoryEmbeddingClient_Embed_Timeout(t *testing.T) {
	// Create a slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(embeddingResponse{})
	}))
	defer server.Close()

	os.Setenv("TEST_TIMEOUT_KEY", "test-key")
	defer os.Unsetenv("TEST_TIMEOUT_KEY")

	// Create client with very short timeout
	client, err := NewAiFactoryEmbeddingClient(config.EmbeddingConfig{
		BaseURL:   server.URL,
		APIKeyEnv: "TEST_TIMEOUT_KEY",
		TimeoutMs: 50, // 50ms timeout
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	_, err = client.Embed(ctx, []string{"test"})

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// Check for timeout-related error message
	errMsg := err.Error()
	if !strings.Contains(errMsg, "timeout") && !strings.Contains(errMsg, "deadline") && !strings.Contains(errMsg, "context") {
		t.Errorf("expected timeout/deadline error, got: %v", err)
	}
}

func TestAiFactoryEmbeddingClient_Embed_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(embeddingResponse{})
	}))
	defer server.Close()

	os.Setenv("TEST_CTX_KEY", "test-key")
	defer os.Unsetenv("TEST_CTX_KEY")

	client, err := NewAiFactoryEmbeddingClient(config.EmbeddingConfig{
		BaseURL:   server.URL,
		APIKeyEnv: "TEST_CTX_KEY",
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Create a context that will be cancelled immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.Embed(ctx, []string{"test"})

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}

	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context cancellation error, got: %v", err)
	}
}
