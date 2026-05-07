package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaEmbedderEmbedsBatch(t *testing.T) {
	var gotPath string
	var gotBody ollamaEmbedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[1,2,3],[4,5,6]]}`))
	}))
	defer srv.Close()

	embedder, err := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL + "/", Model: "nomic-embed-text"})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder failed: %v", err)
	}

	got, err := embedder.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if gotPath != "/api/embed" {
		t.Fatalf("path = %q, want /api/embed", gotPath)
	}
	if gotBody.Model != "nomic-embed-text" || len(gotBody.Input) != 2 || gotBody.Input[0] != "first" || gotBody.Input[1] != "second" {
		t.Fatalf("request body = %+v, want model and ordered inputs", gotBody)
	}
	if len(got) != 2 || got[0][0] != 1 || got[1][2] != 6 {
		t.Fatalf("embeddings = %+v, want response vectors", got)
	}
}

func TestOllamaEmbedderEmptyInputSkipsRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	embedder, err := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "test-model"})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder failed: %v", err)
	}

	got, err := embedder.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if got != nil {
		t.Fatalf("embeddings = %+v, want nil", got)
	}
	if called {
		t.Fatal("server was called for empty input")
	}
}

func TestOllamaEmbedderNon2xxReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model missing", http.StatusNotFound)
	}))
	defer srv.Close()

	embedder, err := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "missing"})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder failed: %v", err)
	}

	_, err = embedder.Embed(context.Background(), []string{"text"})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want ProviderError", err)
	}
	if providerErr.StatusCode != http.StatusNotFound || !strings.Contains(providerErr.Body, "model missing") {
		t.Fatalf("provider error = %+v, want status and body", providerErr)
	}
}

func TestOllamaEmbedderMalformedResponseReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	embedder, err := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "test-model"})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder failed: %v", err)
	}

	_, err = embedder.Embed(context.Background(), []string{"text"})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || !strings.Contains(providerErr.Error(), "decode response") {
		t.Fatalf("error = %v, want decode ProviderError", err)
	}
}

func TestOllamaEmbedderEmptyEmbeddingReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[[]]}`))
	}))
	defer srv.Close()

	embedder, err := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "test-model"})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder failed: %v", err)
	}

	_, err = embedder.Embed(context.Background(), []string{"text"})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || !strings.Contains(providerErr.Error(), "empty embedding") {
		t.Fatalf("error = %v, want empty embedding ProviderError", err)
	}
}

func TestOllamaEmbedderTimeoutReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer srv.Close()

	embedder, err := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "test-model", Timeout: time.Millisecond})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder failed: %v", err)
	}

	_, err = embedder.Embed(context.Background(), []string{"text"})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want ProviderError", err)
	}
}

func TestNewOllamaEmbedderValidatesConfig(t *testing.T) {
	if _, err := NewOllamaEmbedder(OllamaConfig{Model: ""}); err == nil {
		t.Fatal("expected missing model error")
	}
	if _, err := NewOllamaEmbedder(OllamaConfig{BaseURL: "://bad", Model: "test-model"}); err == nil {
		t.Fatal("expected invalid base URL error")
	}
}
