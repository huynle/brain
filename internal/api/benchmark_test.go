package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Benchmark helpers
// ---------------------------------------------------------------------------

// benchMock returns a mockBrainService pre-wired for benchmark scenarios.
// All methods return realistic data without actual storage.
func benchMock(entryCount int) *mockBrainService {
	// Pre-generate entries for list/search responses
	entries := make([]types.BrainEntry, entryCount)
	for i := 0; i < entryCount; i++ {
		entries[i] = types.BrainEntry{
			ID:      fmt.Sprintf("bn%05d", i),
			Path:    fmt.Sprintf("projects/bench/task/note-%04d.md", i),
			Title:   fmt.Sprintf("Benchmark Note %d", i),
			Type:    "task",
			Status:  "active",
			Content: fmt.Sprintf("Body content for benchmark note %d with various keywords.", i),
			Tags:    []string{"go", "api"},
		}
	}

	searchResults := make([]types.SearchResult, min(20, entryCount))
	for i := range searchResults {
		searchResults[i] = types.SearchResult{
			ID:      entries[i].ID,
			Path:    entries[i].Path,
			Title:   entries[i].Title,
			Type:    entries[i].Type,
			Status:  entries[i].Status,
			Snippet: "...benchmark note with various keywords...",
		}
	}

	return &mockBrainService{
		saveFunc: func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
			return &types.CreateEntryResponse{
				ID:     "abc12def",
				Path:   "projects/default/task/test.md",
				Title:  req.Title,
				Type:   req.Type,
				Status: "active",
				Link:   "[Test](abc12def)",
			}, nil
		},
		recallFunc: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
			return &entries[0], nil
		},
		listFunc: func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
			limit := 20
			if req.Limit > 0 {
				limit = req.Limit
			}
			end := min(limit, len(entries))
			return &types.ListEntriesResponse{
				Entries: entries[:end],
				Total:   len(entries),
				Limit:   limit,
				Offset:  req.Offset,
			}, nil
		},
		searchFunc: func(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error) {
			return &types.SearchResponse{
				Results: searchResults,
				Total:   len(searchResults),
			}, nil
		},
	}
}

// benchRouter creates a chi router with all entry + search routes wired to the mock.
func benchRouter(mock *mockBrainService) *chi.Mux {
	h := NewHandler(mock)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", HealthHandler())
		r.Post("/search", h.HandleSearch)
		r.Route("/entries", func(r chi.Router) {
			r.Post("/", h.HandleCreateEntry)
			r.Get("/", h.HandleListEntries)
			r.Get("/{id}", h.HandleGetEntry)
		})
	})
	return r
}

// ---------------------------------------------------------------------------
// API Benchmarks — httptest.NewRecorder (no TCP overhead, pure handler perf)
//
// These measure the handler + router + JSON serialization performance
// without network overhead. This is the recommended way to benchmark
// HTTP handlers in Go.
// ---------------------------------------------------------------------------

func BenchmarkHealthEndpoint(b *testing.B) {
	mock := benchMock(100)
	router := benchRouter(mock)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("health status = %d, want 200", w.Code)
		}
	}
}

func BenchmarkCreateEntry(b *testing.B) {
	mock := benchMock(100)
	router := benchRouter(mock)

	reqBody := map[string]any{
		"type":    "task",
		"title":   "Benchmark Task",
		"content": "Content for benchmark task.",
		"tags":    []string{"go", "benchmark"},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/entries", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			b.Fatalf("create status = %d, want 201", w.Code)
		}
	}
}

func BenchmarkGetEntry(b *testing.B) {
	mock := benchMock(100)
	router := benchRouter(mock)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/entries/abc12def", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("get status = %d, want 200", w.Code)
		}
	}
}

func BenchmarkSearchEndpoint(b *testing.B) {
	mock := benchMock(500)
	router := benchRouter(mock)

	reqBody := map[string]any{
		"query": "authentication database",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("search status = %d, want 200", w.Code)
		}
	}
}

func BenchmarkListEntries(b *testing.B) {
	mock := benchMock(500)
	router := benchRouter(mock)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/entries?type=task&limit=20", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("list status = %d, want 200", w.Code)
		}
	}
}

func BenchmarkListEntriesWithFilters(b *testing.B) {
	mock := benchMock(500)
	router := benchRouter(mock)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/entries?type=task&status=active&limit=20&offset=10&sortBy=modified", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("list with filters status = %d, want 200", w.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// API Benchmarks — httptest.NewServer (end-to-end with TCP)
//
// These measure the full HTTP stack including TCP, connection handling,
// and serialization. Uses a shared client with keep-alive to avoid
// ephemeral port exhaustion.
// ---------------------------------------------------------------------------

func BenchmarkHealthEndpoint_TCP(b *testing.B) {
	mock := benchMock(100)
	router := benchRouter(mock)
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(srv.URL + "/api/v1/health")
		if err != nil {
			b.Fatalf("GET /health: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("health status = %d, want 200", resp.StatusCode)
		}
	}
}

func BenchmarkCreateEntry_TCP(b *testing.B) {
	mock := benchMock(100)
	router := benchRouter(mock)
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	reqBody := map[string]any{
		"type":    "task",
		"title":   "Benchmark Task",
		"content": "Content for benchmark task with various keywords.",
		"tags":    []string{"go", "benchmark"},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Post(srv.URL+"/api/v1/entries", "application/json", bytes.NewReader(bodyBytes))
		if err != nil {
			b.Fatalf("POST /entries: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			b.Fatalf("create status = %d, want 201", resp.StatusCode)
		}
	}
}

func BenchmarkGetEntry_TCP(b *testing.B) {
	mock := benchMock(100)
	router := benchRouter(mock)
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(srv.URL + "/api/v1/entries/abc12def")
		if err != nil {
			b.Fatalf("GET /entries/abc12def: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("get status = %d, want 200", resp.StatusCode)
		}
	}
}

func BenchmarkSearchEndpoint_TCP(b *testing.B) {
	mock := benchMock(500)
	router := benchRouter(mock)
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	reqBody := map[string]any{
		"query": "authentication database",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Post(srv.URL+"/api/v1/search", "application/json", bytes.NewReader(bodyBytes))
		if err != nil {
			b.Fatalf("POST /search: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("search status = %d, want 200", resp.StatusCode)
		}
	}
}

func BenchmarkListEntries_TCP(b *testing.B) {
	mock := benchMock(500)
	router := benchRouter(mock)
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(srv.URL + "/api/v1/entries?type=task&limit=20")
		if err != nil {
			b.Fatalf("GET /entries: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("list status = %d, want 200", resp.StatusCode)
		}
	}
}
