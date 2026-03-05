package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

func testConfig() config.Config {
	return config.Config{
		BrainDir:   "/tmp/test-brain",
		Port:       3000,
		Host:       "0.0.0.0",
		EnableAuth: false,
		APIKey:     "",
		CORSOrigin: "*",
		LogLevel:   "info",
	}
}

func TestHealthEndpoint(t *testing.T) {
	router := NewRouter(testConfig())
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Check Content-Type
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Parse response body
	var health types.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if health.Status != "healthy" {
		t.Errorf("status = %q, want %q", health.Status, "healthy")
	}
	if health.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
}

func TestNotFoundHandler(t *testing.T) {
	router := NewRouter(testConfig())
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/nonexistent")
	if err != nil {
		t.Fatalf("GET /api/v1/nonexistent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	var errResp types.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Error != "Not Found" {
		t.Errorf("error = %q, want %q", errResp.Error, "Not Found")
	}
	if errResp.Message == "" {
		t.Error("message should not be empty")
	}
}

func TestRequestIDHeader(t *testing.T) {
	router := NewRouter(testConfig())
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health failed: %v", err)
	}
	defer resp.Body.Close()

	reqID := resp.Header.Get("X-Request-ID")
	if reqID == "" {
		t.Error("X-Request-ID header should be present")
	}
	// UUID format: 8-4-4-4-12 hex chars
	if len(reqID) != 36 {
		t.Errorf("X-Request-ID length = %d, want 36 (UUID format)", len(reqID))
	}
}

func TestCORSHeaders(t *testing.T) {
	router := NewRouter(testConfig())
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health failed: %v", err)
	}
	defer resp.Body.Close()

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", origin, "*")
	}

	methods := resp.Header.Get("Access-Control-Allow-Methods")
	if methods == "" {
		t.Error("Access-Control-Allow-Methods should be present")
	}
}

func TestCORSPreflight(t *testing.T) {
	router := NewRouter(testConfig())
	srv := httptest.NewServer(router)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/api/v1/health", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /api/v1/health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestSecureHeaders(t *testing.T) {
	router := NewRouter(testConfig())
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health failed: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
	}
}

func TestNotImplementedRoutes(t *testing.T) {
	router := NewRouter(testConfig())
	srv := httptest.NewServer(router)
	defer srv.Close()

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/stats"},
		{"GET", "/api/v1/orphans"},
		{"GET", "/api/v1/stale"},
		{"POST", "/api/v1/search"},
		{"POST", "/api/v1/inject"},
		{"GET", "/api/v1/entries"},
		{"POST", "/api/v1/entries"},
		{"GET", "/api/v1/tasks"},
	}

	for _, tt := range routes {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, srv.URL+tt.path, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s failed: %v", tt.method, tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
			}

			var errResp types.ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				t.Fatalf("failed to decode error response: %v", err)
			}
			if errResp.Error != "Not Implemented" {
				t.Errorf("error = %q, want %q", errResp.Error, "Not Implemented")
			}
		})
	}
}

func TestAuthMiddleware_Disabled(t *testing.T) {
	cfg := testConfig()
	cfg.EnableAuth = false
	router := NewRouter(cfg)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Should pass without any auth header
	resp, err := http.Get(srv.URL + "/api/v1/stats")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should get 501 (not implemented), not 401
	if resp.StatusCode == http.StatusUnauthorized {
		t.Error("should not get 401 when auth is disabled")
	}
}

func TestAuthMiddleware_Enabled(t *testing.T) {
	cfg := testConfig()
	cfg.EnableAuth = true
	cfg.APIKey = "test-secret-key"
	router := NewRouter(cfg)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// No token → 401
	t.Run("no token", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v1/stats")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	// Wrong token → 401
	t.Run("wrong token", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/stats", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	// Correct Bearer token → passes through (501 for unimplemented)
	t.Run("correct bearer token", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/stats", nil)
		req.Header.Set("Authorization", "Bearer test-secret-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			t.Error("should not get 401 with correct token")
		}
	})

	// Correct query param token → passes through
	t.Run("correct query param token", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v1/stats?token=test-secret-key")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			t.Error("should not get 401 with correct query token")
		}
	})

	// Health endpoint should be accessible without auth
	t.Run("health bypasses auth", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v1/health")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})
}

func TestCORSCustomOrigin(t *testing.T) {
	cfg := testConfig()
	cfg.CORSOrigin = "https://example.com"
	router := NewRouter(cfg)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != "https://example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", origin, "https://example.com")
	}

	creds := resp.Header.Get("Access-Control-Allow-Credentials")
	if creds != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want %q", creds, "true")
	}
}
