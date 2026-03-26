package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock validator
// ---------------------------------------------------------------------------

type mockAccessTokenValidator struct {
	tokens map[string]*storage.OAuthAccessToken
}

func newMockValidator() *mockAccessTokenValidator {
	return &mockAccessTokenValidator{
		tokens: make(map[string]*storage.OAuthAccessToken),
	}
}

func (m *mockAccessTokenValidator) addToken(token string, clientID, scope string) {
	m.tokens[token] = &storage.OAuthAccessToken{
		Token:    token,
		ClientID: clientID,
		Scope:    scope,
	}
}

func (m *mockAccessTokenValidator) GetAccessToken(_ context.Context, tokenValue string) (*storage.OAuthAccessToken, error) {
	t, ok := m.tokens[tokenValue]
	if !ok {
		return nil, fmt.Errorf("access token not found: %s", tokenValue)
	}
	return t, nil
}

// okHandler is a simple handler that returns 200 OK.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// ---------------------------------------------------------------------------
// BearerAuth tests
// ---------------------------------------------------------------------------

func TestBearerAuth_ValidToken(t *testing.T) {
	v := newMockValidator()
	v.addToken("valid-token-123", "client-abc", "mcp read")

	handler := BearerAuth(v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestBearerAuth_MissingToken(t *testing.T) {
	v := newMockValidator()

	handler := BearerAuth(v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), `Bearer realm="brain-api"`) {
		t.Fatal("expected WWW-Authenticate header with Bearer realm")
	}
}

func TestBearerAuth_InvalidToken(t *testing.T) {
	v := newMockValidator()

	handler := BearerAuth(v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "invalid_token") {
		t.Fatal("expected WWW-Authenticate header with invalid_token error")
	}
}

func TestBearerAuth_SetsContext(t *testing.T) {
	v := newMockValidator()
	v.addToken("ctx-token", "my-client", "mcp read write")

	var gotClientID, gotScope string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClientID, _ = OAuthClientFromContext(r.Context())
		gotScope, _ = OAuthScopeFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := BearerAuth(v)(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer ctx-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotClientID != "my-client" {
		t.Fatalf("expected client ID 'my-client', got '%s'", gotClientID)
	}
	if gotScope != "mcp read write" {
		t.Fatalf("expected scope 'mcp read write', got '%s'", gotScope)
	}
}

func TestBearerAuth_CaseInsensitiveBearer(t *testing.T) {
	v := newMockValidator()
	v.addToken("lower-token", "client-1", "mcp")

	handler := BearerAuth(v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "bearer lower-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for lowercase 'bearer', got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// OptionalBearerAuth tests
// ---------------------------------------------------------------------------

func TestOptionalBearerAuth_NoToken(t *testing.T) {
	v := newMockValidator()

	handler := OptionalBearerAuth(v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (pass-through), got %d", rec.Code)
	}
}

func TestOptionalBearerAuth_ValidToken(t *testing.T) {
	v := newMockValidator()
	v.addToken("opt-valid", "client-opt", "read")

	var gotClientID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClientID, _ = OAuthClientFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := OptionalBearerAuth(v)(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer opt-valid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotClientID != "client-opt" {
		t.Fatalf("expected client ID 'client-opt', got '%s'", gotClientID)
	}
}

func TestOptionalBearerAuth_InvalidToken(t *testing.T) {
	v := newMockValidator()

	handler := OptionalBearerAuth(v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-opt-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", rec.Code)
	}
}

func TestOptionalBearerAuth_NoContextWithoutToken(t *testing.T) {
	v := newMockValidator()

	var hasClient, hasScope bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasClient = OAuthClientFromContext(r.Context())
		_, hasScope = OAuthScopeFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := OptionalBearerAuth(v)(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if hasClient {
		t.Fatal("expected no client ID in context without token")
	}
	if hasScope {
		t.Fatal("expected no scope in context without token")
	}
}

// ---------------------------------------------------------------------------
// RequireScope tests
// ---------------------------------------------------------------------------

func TestRequireScope_HasRequiredScope(t *testing.T) {
	handler := RequireScope("read", "write")(okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	ctx := context.WithValue(req.Context(), ctxOAuthScope, "read write admin")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRequireScope_MissingScope(t *testing.T) {
	handler := RequireScope("admin")(okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	ctx := context.WithValue(req.Context(), ctxOAuthScope, "read write")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequireScope_MCPParentImpliesSubScopes(t *testing.T) {
	tests := []struct {
		name     string
		granted  string
		required string
		allowed  bool
	}{
		{"mcp grants mcp:read", "mcp", "mcp:read", true},
		{"mcp grants mcp:write", "mcp", "mcp:write", true},
		{"mcp grants mcp:admin", "mcp", "mcp:admin", true},
		{"mcp:read does not grant mcp", "mcp:read", "mcp", false},
		{"mcp:read does not grant mcp:write", "mcp:read", "mcp:write", false},
		{"exact mcp:read grants mcp:read", "mcp:read", "mcp:read", true},
		{"mcp grants mcp itself", "mcp", "mcp", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := RequireScope(tt.required)(okHandler())

			req := httptest.NewRequest("GET", "/test", nil)
			ctx := context.WithValue(req.Context(), ctxOAuthScope, tt.granted)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if tt.allowed && rec.Code != http.StatusOK {
				t.Fatalf("expected 200 (allowed), got %d", rec.Code)
			}
			if !tt.allowed && rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 (denied), got %d", rec.Code)
			}
		})
	}
}

func TestRequireScope_NoScopeInContext(t *testing.T) {
	handler := RequireScope("read")(okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when no scope in context, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ConditionalAuth tests
// ---------------------------------------------------------------------------

func TestConditionalAuth_Enabled(t *testing.T) {
	v := newMockValidator()
	// No tokens in validator — should reject

	handler := ConditionalAuth(true, v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when enabled and no token, got %d", rec.Code)
	}
}

func TestConditionalAuth_Disabled(t *testing.T) {
	v := newMockValidator()

	handler := ConditionalAuth(false, v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (pass-through) when disabled, got %d", rec.Code)
	}
}

func TestConditionalAuth_EnabledWithValidToken(t *testing.T) {
	v := newMockValidator()
	v.addToken("cond-token", "cond-client", "mcp")

	handler := ConditionalAuth(true, v)(okHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer cond-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when enabled with valid token, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Context accessor tests
// ---------------------------------------------------------------------------

func TestOAuthClientFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	_, ok := OAuthClientFromContext(ctx)
	if ok {
		t.Fatal("expected ok=false when no client ID in context")
	}
}

func TestOAuthScopeFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	_, ok := OAuthScopeFromContext(ctx)
	if ok {
		t.Fatal("expected ok=false when no scope in context")
	}
}

// ---------------------------------------------------------------------------
// extractBearer tests
// ---------------------------------------------------------------------------

func TestExtractBearer(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer abc123", "abc123"},
		{"bearer abc123", "abc123"},
		{"Basic abc123", ""},
		{"", ""},
		{"Bearer ", ""},
		{"Bearertoken", ""},
	}

	for _, tt := range tests {
		got := extractBearer(tt.header)
		if got != tt.want {
			t.Errorf("extractBearer(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// hasScope tests
// ---------------------------------------------------------------------------

func TestHasScope(t *testing.T) {
	tests := []struct {
		name     string
		granted  map[string]bool
		required string
		want     bool
	}{
		{"exact match", map[string]bool{"read": true}, "read", true},
		{"missing scope", map[string]bool{"read": true}, "write", false},
		{"mcp implies mcp:read", map[string]bool{"mcp": true}, "mcp:read", true},
		{"mcp implies mcp:write", map[string]bool{"mcp": true}, "mcp:write", true},
		{"mcp:read does not imply mcp:write", map[string]bool{"mcp:read": true}, "mcp:write", false},
		{"empty granted set", map[string]bool{}, "read", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasScope(tt.granted, tt.required)
			if got != tt.want {
				t.Errorf("hasScope(%v, %q) = %v, want %v", tt.granted, tt.required, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration: BearerAuth + RequireScope chain
// ---------------------------------------------------------------------------

func TestBearerAuthWithRequireScope(t *testing.T) {
	v := newMockValidator()
	v.addToken("scoped-token", "scope-client", "mcp read")

	handler := BearerAuth(v)(RequireScope("mcp:write")(okHandler()))
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// "mcp" scope should grant "mcp:write" via parent scope rule
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (mcp implies mcp:write), got %d", rec.Code)
	}
}

func TestBearerAuthWithRequireScope_Insufficient(t *testing.T) {
	v := newMockValidator()
	v.addToken("limited-token", "limited-client", "read")

	handler := BearerAuth(v)(RequireScope("admin")(okHandler()))
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer limited-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (missing admin scope), got %d", rec.Code)
	}
}
