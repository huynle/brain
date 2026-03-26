package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
)

// testValidator is a mock TokenValidator for middleware tests.
type testValidator struct {
	validToken string
}

func (v *testValidator) ValidateToken(_ context.Context, tokenValue string) (*storage.Token, error) {
	if tokenValue == v.validToken {
		return &storage.Token{Name: "test-token", Token: tokenValue}, nil
	}
	return nil, fmt.Errorf("token not found or revoked")
}

// revokedValidator always returns an error, simulating a revoked token.
type revokedValidator struct{}

func (v *revokedValidator) ValidateToken(_ context.Context, _ string) (*storage.Token, error) {
	return nil, fmt.Errorf("token revoked")
}

// okHandler is a simple handler that writes 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
})

func TestAuth_Disabled(t *testing.T) {
	// When auth is disabled, all requests should pass through regardless of token.
	middleware := Auth(false, &testValidator{validToken: "secret"})
	handler := middleware(okHandler)

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuth_ValidToken(t *testing.T) {
	middleware := Auth(true, &testValidator{validToken: "valid-token-123"})
	handler := middleware(okHandler)

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer valid-token-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	middleware := Auth(true, &testValidator{validToken: "valid-token-123"})
	handler := middleware(okHandler)

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("expected WWW-Authenticate header")
	}
	if wwwAuth != `Bearer realm="brain-api", error="invalid_token"` {
		t.Errorf("WWW-Authenticate = %q, want %q", wwwAuth, `Bearer realm="brain-api", error="invalid_token"`)
	}
}

func TestAuth_MissingToken(t *testing.T) {
	middleware := Auth(true, &testValidator{validToken: "valid-token-123"})
	handler := middleware(okHandler)

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth != `Bearer realm="brain-api"` {
		t.Errorf("WWW-Authenticate = %q, want %q", wwwAuth, `Bearer realm="brain-api"`)
	}
}

func TestAuth_RevokedToken(t *testing.T) {
	middleware := Auth(true, &revokedValidator{})
	handler := middleware(okHandler)

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer some-revoked-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuth_BearerCaseInsensitive(t *testing.T) {
	middleware := Auth(true, &testValidator{validToken: "my-token"})
	handler := middleware(okHandler)

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"uppercase Bearer", "Bearer my-token", http.StatusOK},
		{"lowercase bearer", "bearer my-token", http.StatusOK},
		{"no prefix", "my-token", http.StatusUnauthorized},
		{"BEARER uppercase", "BEARER my-token", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestAuth_QueryParamFallback(t *testing.T) {
	middleware := Auth(true, &testValidator{validToken: "query-token"})
	handler := middleware(okHandler)

	req := httptest.NewRequest("GET", "/test?token=query-token", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuth_HeaderTakesPrecedence(t *testing.T) {
	middleware := Auth(true, &testValidator{validToken: "header-token"})
	handler := middleware(okHandler)

	// Header has the valid token, query param has an invalid one.
	// Header should take precedence and succeed.
	req := httptest.NewRequest("GET", "/test?token=wrong-query-token", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (header should take precedence)", rec.Code, http.StatusOK)
	}
}

func TestAuth_TokenSanitizationInLogs(t *testing.T) {
	// This test verifies that the Logger middleware sanitizes token query params.
	// We test the tokenPattern regex directly.
	tests := []struct {
		input string
		want  string
	}{
		{"token=abc123", "token=***"},
		{"foo=bar&token=abc123", "foo=bar&token=***"},
		{"token=abc123&bar=baz", "token=***&bar=baz"},
		{"foo=bar", "foo=bar"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tokenPattern.ReplaceAllString(tt.input, "${1}token=***")
			if got != tt.want {
				t.Errorf("sanitize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer abc123", "abc123"},
		{"bearer abc123", "abc123"},
		{"BEARER abc123", ""},
		{"Basic abc123", ""},
		{"", ""},
		{"Bearer ", ""},
		{"bearer ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := extractBearerToken(tt.header)
			if got != tt.want {
				t.Errorf("extractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}
