package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIClient_Request_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("path = %q, want /api/v1/health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL)
	var result map[string]string
	err := client.Request(context.Background(), "GET", "/health", nil, nil, &result)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("status = %q, want %q", result["status"], "ok")
	}
}

func TestAPIClient_Request_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/entries" {
			t.Errorf("path = %q, want /api/v1/entries", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["title"] != "test" {
			t.Errorf("body.title = %q, want %q", body["title"], "test")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "abc12345", "path": "projects/test/summary/abc12345.md"})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL)
	body := map[string]string{"title": "test"}
	var result map[string]string
	err := client.Request(context.Background(), "POST", "/entries", body, nil, &result)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if result["id"] != "abc12345" {
		t.Errorf("id = %q, want %q", result["id"], "abc12345")
	}
}

func TestAPIClient_Request_QueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "task" {
			t.Errorf("query type = %q, want %q", r.URL.Query().Get("type"), "task")
		}
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("query limit = %q, want %q", r.URL.Query().Get("limit"), "10")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"total": 5})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL)
	params := map[string]string{"type": "task", "limit": "10"}
	var result map[string]int
	err := client.Request(context.Background(), "GET", "/entries", nil, params, &result)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if result["total"] != 5 {
		t.Errorf("total = %d, want %d", result["total"], 5)
	}
}

func TestAPIClient_Request_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found", "message": "Entry not found"})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL)
	var result map[string]any
	err := client.Request(context.Background(), "GET", "/entries/nonexistent", nil, nil, &result)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if err.Error() != "Entry not found" {
		t.Errorf("error = %q, want %q", err.Error(), "Entry not found")
	}
}

func TestAPIClient_Request_ConnectionError(t *testing.T) {
	client := NewAPIClient("http://localhost:1") // Port 1 should refuse connections
	var result map[string]any
	err := client.Request(context.Background(), "GET", "/health", nil, nil, &result)
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
}

func TestAPIClient_Request_DELETE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if r.URL.Query().Get("confirm") != "true" {
			t.Errorf("query confirm = %q, want %q", r.URL.Query().Get("confirm"), "true")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "deleted"})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL)
	params := map[string]string{"confirm": "true"}
	var result map[string]string
	err := client.Request(context.Background(), "DELETE", "/entries/test-path", nil, params, &result)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if result["message"] != "deleted" {
		t.Errorf("message = %q, want %q", result["message"], "deleted")
	}
}

func TestAPIClient_Request_PATCH(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["status"] != "completed" {
			t.Errorf("body.status = %q, want %q", body["status"], "completed")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL)
	body := map[string]string{"status": "completed"}
	var result map[string]string
	err := client.Request(context.Background(), "PATCH", "/entries/test-path", body, nil, &result)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if result["status"] != "completed" {
		t.Errorf("status = %q, want %q", result["status"], "completed")
	}
}
