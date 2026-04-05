package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock validators
// ---------------------------------------------------------------------------

// testValidator is a mock TokenValidator that accepts a single valid token.
type testValidator struct {
	validToken string
	scope      string // optional: scope to assign (defaults to "admin:*")
}

func (v *testValidator) ValidateToken(_ context.Context, tokenValue string) (*storage.Token, error) {
	if tokenValue == v.validToken {
		scope := v.scope
		if scope == "" {
			scope = "admin:*"
		}
		return &storage.Token{Name: "test-token", Token: tokenValue, Scope: scope}, nil
	}
	return nil, fmt.Errorf("token not found or revoked")
}

// scopedTokenValidator accepts multiple tokens with different scopes.
type scopedTokenValidator struct {
	tokens map[string]string // token value -> scope
}

func newScopedTokenValidator() *scopedTokenValidator {
	return &scopedTokenValidator{tokens: make(map[string]string)}
}

func (v *scopedTokenValidator) addToken(token, scope string) {
	v.tokens[token] = scope
}

func (v *scopedTokenValidator) ValidateToken(_ context.Context, tokenValue string) (*storage.Token, error) {
	scope, ok := v.tokens[tokenValue]
	if !ok {
		return nil, fmt.Errorf("token not found or revoked")
	}
	return &storage.Token{Name: "token-" + scope, Token: tokenValue, Scope: scope}, nil
}

// revokedValidator always returns an error, simulating a revoked/invalid token.
type revokedValidator struct{}

func (v *revokedValidator) ValidateToken(_ context.Context, _ string) (*storage.Token, error) {
	return nil, fmt.Errorf("token revoked")
}

// testOAuthValidator is a mock OAuthAccessTokenValidator for middleware tests.
type testOAuthValidator struct {
	tokens map[string]*storage.OAuthAccessToken
}

func newTestOAuthValidator() *testOAuthValidator {
	return &testOAuthValidator{tokens: make(map[string]*storage.OAuthAccessToken)}
}

func (v *testOAuthValidator) addToken(token, clientID, scope string) {
	v.tokens[token] = &storage.OAuthAccessToken{
		Token:    token,
		ClientID: clientID,
		Scope:    scope,
	}
}

func (v *testOAuthValidator) GetAccessToken(_ context.Context, tokenValue string) (*storage.OAuthAccessToken, error) {
	tok, ok := v.tokens[tokenValue]
	if !ok {
		return nil, fmt.Errorf("access token not found")
	}
	return tok, nil
}

// expiredOAuthValidator always returns an expiry error, simulating an expired OAuth token.
type expiredOAuthValidator struct{}

func (v *expiredOAuthValidator) GetAccessToken(_ context.Context, _ string) (*storage.OAuthAccessToken, error) {
	return nil, fmt.Errorf("access token expired")
}

// okHandler is a simple handler that writes 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
})

// contextCapturingHandler captures the AuthResult from the request context.
func contextCapturingHandler(captured **AuthResult) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, ok := AuthResultFromContext(r.Context())
		if ok {
			*captured = result
		}
		w.WriteHeader(http.StatusOK)
	})
}

// ---------------------------------------------------------------------------
// Test 1-7, 9, 11: Table-driven auth middleware tests
// ---------------------------------------------------------------------------

func TestAuthMiddleware(t *testing.T) {
	apiValidator := &testValidator{validToken: "valid-api-token"}
	oauthValidator := newTestOAuthValidator()
	oauthValidator.addToken("valid-oauth-token", "client-abc", "read write")

	composite := &CompositeValidator{
		APIValidator:   apiValidator,
		OAuthValidator: oauthValidator,
	}

	tests := []struct {
		name         string
		enabled      bool
		validator    TokenValidator
		header       string
		queryToken   string
		wantStatus   int
		wantAuthType string // expected authType in context ("" means not checked)
		wantWWWAuth  string // expected WWW-Authenticate header ("" means not checked)
	}{
		// Test 1: Auth disabled — all requests pass through regardless of token
		{
			name:       "auth disabled - no token passes through",
			enabled:    false,
			validator:  composite,
			header:     "",
			queryToken: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "auth disabled - invalid token still passes through",
			enabled:    false,
			validator:  composite,
			header:     "Bearer totally-wrong",
			queryToken: "",
			wantStatus: http.StatusOK,
		},
		// Test 2: No token — returns 401 with WWW-Authenticate header
		{
			name:        "no token returns 401",
			enabled:     true,
			validator:   composite,
			header:      "",
			queryToken:  "",
			wantStatus:  http.StatusUnauthorized,
			wantWWWAuth: `Bearer realm="brain-api"`,
		},
		// Test 3: Valid API token — passes through, context set with authType=api_token
		{
			name:         "valid API token passes through",
			enabled:      true,
			validator:    composite,
			header:       "Bearer valid-api-token",
			queryToken:   "",
			wantStatus:   http.StatusOK,
			wantAuthType: "api_token",
		},
		// Test 4: Valid OAuth token — passes through, context set with authType=oauth
		{
			name:         "valid OAuth token passes through",
			enabled:      true,
			validator:    composite,
			header:       "Bearer valid-oauth-token",
			queryToken:   "",
			wantStatus:   http.StatusOK,
			wantAuthType: "oauth",
		},
		// Test 5: Invalid token — returns 401 (rejected by both validators)
		{
			name:        "invalid token returns 401",
			enabled:     true,
			validator:   composite,
			header:      "Bearer totally-wrong-token",
			queryToken:  "",
			wantStatus:  http.StatusUnauthorized,
			wantWWWAuth: `Bearer realm="brain-api", error="invalid_token"`,
		},
		// Test 6: Query param auth (?token=xxx) — works as fallback
		{
			name:         "query param token works as fallback",
			enabled:      true,
			validator:    composite,
			header:       "",
			queryToken:   "valid-api-token",
			wantStatus:   http.StatusOK,
			wantAuthType: "api_token",
		},
		// Test 7: Header precedence — Authorization header takes priority over query param
		{
			name:         "header takes precedence over query param",
			enabled:      true,
			validator:    composite,
			header:       "Bearer valid-api-token",
			queryToken:   "totally-wrong-token",
			wantStatus:   http.StatusOK,
			wantAuthType: "api_token",
		},
		// Test 9: Revoked API token — returns 401
		{
			name:        "revoked API token returns 401",
			enabled:     true,
			validator:   &revokedValidator{},
			header:      "Bearer some-revoked-token",
			queryToken:  "",
			wantStatus:  http.StatusUnauthorized,
			wantWWWAuth: `Bearer realm="brain-api", error="invalid_token"`,
		},
		// Test 11: Case-insensitive bearer/Bearer — both work
		{
			name:         "uppercase Bearer works",
			enabled:      true,
			validator:    composite,
			header:       "Bearer valid-api-token",
			queryToken:   "",
			wantStatus:   http.StatusOK,
			wantAuthType: "api_token",
		},
		{
			name:         "lowercase bearer works",
			enabled:      true,
			validator:    composite,
			header:       "bearer valid-api-token",
			queryToken:   "",
			wantStatus:   http.StatusOK,
			wantAuthType: "api_token",
		},
		{
			name:       "BEARER uppercase rejected (only Bearer/bearer supported)",
			enabled:    true,
			validator:  composite,
			header:     "BEARER valid-api-token",
			queryToken: "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedResult *AuthResult
			handler := contextCapturingHandler(&capturedResult)

			middleware := Auth(tt.enabled, tt.validator)
			wrapped := middleware(handler)

			path := "/api/v1/test"
			if tt.queryToken != "" {
				path += "?token=" + tt.queryToken
			}

			req := httptest.NewRequest("GET", path, nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantWWWAuth != "" {
				got := rec.Header().Get("WWW-Authenticate")
				if got != tt.wantWWWAuth {
					t.Errorf("WWW-Authenticate = %q, want %q", got, tt.wantWWWAuth)
				}
			}

			if tt.wantAuthType != "" {
				if capturedResult == nil {
					t.Fatal("expected AuthResult in context, got nil")
				}
				if capturedResult.Type != tt.wantAuthType {
					t.Errorf("authType = %q, want %q", capturedResult.Type, tt.wantAuthType)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 8: Health endpoint bypass — /api/v1/health accessible without auth
// ---------------------------------------------------------------------------

func TestAuthMiddleware_HealthEndpointBypass(t *testing.T) {
	// The health endpoint is placed before the auth middleware group in the
	// router, so it bypasses authentication entirely. This test verifies
	// that behavior through the full router stack.
	cfg := config.Config{
		BrainDir:   "/tmp/test-brain",
		Port:       3000,
		Host:       "0.0.0.0",
		EnableAuth: true,
		CORSOrigin: "*",
		LogLevel:   "info",
	}

	validator := &testValidator{validToken: "secret-key"}
	router := NewRouter(cfg, WithTokenValidator(validator))
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Health should be accessible WITHOUT any auth token
	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify it actually returns healthy JSON
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if body["status"] != "healthy" {
		t.Errorf("health status = %q, want %q", body["status"], "healthy")
	}

	// Meanwhile, a protected endpoint should return 401 without token
	resp2, err := http.Get(srv.URL + "/api/v1/stats")
	if err != nil {
		t.Fatalf("GET /api/v1/stats failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("stats status = %d, want %d (should require auth)", resp2.StatusCode, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// Test 10: Expired OAuth token — returns 401
// ---------------------------------------------------------------------------

func TestAuthMiddleware_ExpiredOAuthToken(t *testing.T) {
	// CompositeValidator with an API validator that rejects everything and
	// an OAuth validator that always returns "expired".
	composite := &CompositeValidator{
		APIValidator:   &revokedValidator{},      // rejects all API tokens
		OAuthValidator: &expiredOAuthValidator{}, // returns expired error
	}

	middleware := Auth(true, composite)
	handler := middleware(okHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer expired-oauth-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(wwwAuth, "invalid_token") {
		t.Errorf("WWW-Authenticate = %q, want it to contain 'invalid_token'", wwwAuth)
	}
}

// ---------------------------------------------------------------------------
// Test 12: Token sanitization in logs — ?token= values masked
// ---------------------------------------------------------------------------

func TestAuthMiddleware_TokenSanitizationInLogs(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"token=abc123", "token=***"},
		{"foo=bar&token=abc123", "foo=bar&token=***"},
		{"token=abc123&bar=baz", "token=***&bar=baz"},
		{"foo=bar", "foo=bar"},
		{"token=a-very-long-secret-key-12345", "token=***"},
		{"token=short&token=another", "token=***&token=***"},
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

// ---------------------------------------------------------------------------
// Test 13: Multiple skip paths — configurable paths bypass auth
// ---------------------------------------------------------------------------

// Note: The current Auth middleware does not have built-in skip path support;
// bypass is achieved architecturally by placing routes outside the auth
// middleware group in the router. This test verifies that pattern by
// confirming that the health endpoint (outside auth group) is accessible
// and that other endpoints (inside auth group) are protected.
func TestAuthMiddleware_MultipleSkipPaths(t *testing.T) {
	cfg := config.Config{
		BrainDir:   "/tmp/test-brain",
		Port:       3000,
		Host:       "0.0.0.0",
		EnableAuth: true,
		CORSOrigin: "*",
		LogLevel:   "info",
	}

	validator := &testValidator{validToken: "secret-key"}
	router := NewRouter(cfg, WithTokenValidator(validator))
	srv := httptest.NewServer(router)
	defer srv.Close()

	skipPaths := []struct {
		path       string
		wantStatus int
		desc       string
	}{
		// Health is outside the auth group → accessible without auth
		{"/api/v1/health", http.StatusOK, "health endpoint bypasses auth"},
	}

	protectedPaths := []struct {
		path       string
		wantStatus int
		desc       string
	}{
		// These are inside the auth group → require auth
		{"/api/v1/stats", http.StatusUnauthorized, "stats requires auth"},
		{"/api/v1/entries", http.StatusUnauthorized, "entries requires auth"},
		{"/api/v1/tasks", http.StatusUnauthorized, "tasks requires auth"},
	}

	for _, tt := range skipPaths {
		t.Run(tt.desc, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("%s: status = %d, want %d", tt.path, resp.StatusCode, tt.wantStatus)
			}
		})
	}

	for _, tt := range protectedPaths {
		t.Run(tt.desc, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("%s: status = %d, want %d", tt.path, resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractBearerToken unit tests
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// buildAuthResult unit tests
// ---------------------------------------------------------------------------

func TestBuildAuthResult_APIToken(t *testing.T) {
	tok := &storage.Token{Name: "my-api-key", Token: "abc123"}
	result := buildAuthResult(tok)

	if result.Type != "api_token" {
		t.Errorf("type = %q, want %q", result.Type, "api_token")
	}
	if result.Name != "my-api-key" {
		t.Errorf("name = %q, want %q", result.Name, "my-api-key")
	}
	if result.ClientID != "" {
		t.Errorf("clientID = %q, want empty", result.ClientID)
	}
}

func TestBuildAuthResult_OAuth(t *testing.T) {
	tok := &storage.Token{Name: "oauth:client-id-123:read write mcp"}
	result := buildAuthResult(tok)

	if result.Type != "oauth" {
		t.Errorf("type = %q, want %q", result.Type, "oauth")
	}
	if result.ClientID != "client-id-123" {
		t.Errorf("clientID = %q, want %q", result.ClientID, "client-id-123")
	}
	if result.Scope != "read write mcp" {
		t.Errorf("scope = %q, want %q", result.Scope, "read write mcp")
	}
}

func TestBuildAuthResult_OAuthEmptyScope(t *testing.T) {
	tok := &storage.Token{Name: "oauth:client-id-123:"}
	result := buildAuthResult(tok)

	if result.Type != "oauth" {
		t.Errorf("type = %q, want %q", result.Type, "oauth")
	}
	if result.ClientID != "client-id-123" {
		t.Errorf("clientID = %q, want %q", result.ClientID, "client-id-123")
	}
	if result.Scope != "" {
		t.Errorf("scope = %q, want empty", result.Scope)
	}
}

// ---------------------------------------------------------------------------
// CompositeValidator unit tests
// ---------------------------------------------------------------------------

func TestCompositeValidator_APITokenFirst(t *testing.T) {
	apiValidator := &testValidator{validToken: "api-token-123"}
	oauthValidator := newTestOAuthValidator()
	oauthValidator.addToken("oauth-token-456", "client-abc", "read write")

	composite := &CompositeValidator{
		APIValidator:   apiValidator,
		OAuthValidator: oauthValidator,
	}

	// API token should be validated first
	tok, err := composite.ValidateToken(context.Background(), "api-token-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Name != "test-token" {
		t.Errorf("expected api token name 'test-token', got %q", tok.Name)
	}
}

func TestCompositeValidator_OAuthFallback(t *testing.T) {
	apiValidator := &testValidator{validToken: "api-token-123"}
	oauthValidator := newTestOAuthValidator()
	oauthValidator.addToken("oauth-token-456", "client-abc", "read write")

	composite := &CompositeValidator{
		APIValidator:   apiValidator,
		OAuthValidator: oauthValidator,
	}

	// OAuth token should validate as fallback
	tok, err := composite.ValidateToken(context.Background(), "oauth-token-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Name is encoded as "oauth:<clientID>:<scope>"
	if tok.Name != "oauth:client-abc:read write" {
		t.Errorf("expected encoded oauth name, got %q", tok.Name)
	}
}

func TestCompositeValidator_BothReject(t *testing.T) {
	apiValidator := &testValidator{validToken: "api-token-123"}
	oauthValidator := newTestOAuthValidator()

	composite := &CompositeValidator{
		APIValidator:   apiValidator,
		OAuthValidator: oauthValidator,
	}

	// Neither validator should accept this token
	_, err := composite.ValidateToken(context.Background(), "unknown-token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Dual-auth context verification tests
// ---------------------------------------------------------------------------

func TestAuth_DualAuth_APIToken_Context(t *testing.T) {
	apiValidator := &testValidator{validToken: "api-key"}
	oauthValidator := newTestOAuthValidator()
	oauthValidator.addToken("oauth-key", "client-xyz", "mcp")

	composite := &CompositeValidator{
		APIValidator:   apiValidator,
		OAuthValidator: oauthValidator,
	}

	var capturedResult *AuthResult
	handler := contextCapturingHandler(&capturedResult)

	middleware := Auth(true, composite)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer api-key")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedResult == nil {
		t.Fatal("expected AuthResult in context, got nil")
	}
	if capturedResult.Type != "api_token" {
		t.Errorf("auth type = %q, want %q", capturedResult.Type, "api_token")
	}
	if capturedResult.Name != "test-token" {
		t.Errorf("auth name = %q, want %q", capturedResult.Name, "test-token")
	}
}

func TestAuth_DualAuth_OAuthToken_Context(t *testing.T) {
	apiValidator := &testValidator{validToken: "api-key"}
	oauthValidator := newTestOAuthValidator()
	oauthValidator.addToken("oauth-key", "client-xyz", "mcp read")

	composite := &CompositeValidator{
		APIValidator:   apiValidator,
		OAuthValidator: oauthValidator,
	}

	var capturedResult *AuthResult
	handler := contextCapturingHandler(&capturedResult)

	middleware := Auth(true, composite)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer oauth-key")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedResult == nil {
		t.Fatal("expected AuthResult in context, got nil")
	}
	if capturedResult.Type != "oauth" {
		t.Errorf("auth type = %q, want %q", capturedResult.Type, "oauth")
	}
	if capturedResult.ClientID != "client-xyz" {
		t.Errorf("client ID = %q, want %q", capturedResult.ClientID, "client-xyz")
	}
	if capturedResult.Scope != "mcp read" {
		t.Errorf("scope = %q, want %q", capturedResult.Scope, "mcp read")
	}
}

func TestAuth_DualAuth_InvalidToken_Returns401(t *testing.T) {
	apiValidator := &testValidator{validToken: "api-key"}
	oauthValidator := newTestOAuthValidator()

	composite := &CompositeValidator{
		APIValidator:   apiValidator,
		OAuthValidator: oauthValidator,
	}

	middleware := Auth(true, composite)
	wrapped := middleware(okHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer totally-wrong")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// isTLS unit tests
// ---------------------------------------------------------------------------

func TestIsTLS(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{
			name:    "plain HTTP no headers",
			headers: nil,
			want:    false,
		},
		{
			name:    "X-Forwarded-Proto https",
			headers: map[string]string{"X-Forwarded-Proto": "https"},
			want:    true,
		},
		{
			name:    "X-Forwarded-Proto http",
			headers: map[string]string{"X-Forwarded-Proto": "http"},
			want:    false,
		},
		{
			name:    "X-Forwarded-Ssl on",
			headers: map[string]string{"X-Forwarded-Ssl": "on"},
			want:    true,
		},
		{
			name:    "X-Forwarded-Ssl off",
			headers: map[string]string{"X-Forwarded-Ssl": "off"},
			want:    false,
		},
		{
			name:    "Front-End-Https on",
			headers: map[string]string{"Front-End-Https": "on"},
			want:    true,
		},
		{
			name:    "Front-End-Https off",
			headers: map[string]string{"Front-End-Https": "off"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			got := isTLS(req)
			if got != tt.want {
				t.Errorf("isTLS() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SecureHeaders HSTS tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// RequireScope middleware tests
// ---------------------------------------------------------------------------

func TestRequireScope(t *testing.T) {
	tests := []struct {
		name       string
		allowed    []string // scopes accepted by the middleware
		tokenScope string   // scope of the authenticating token
		authType   string   // "api_token" or "oauth"
		wantStatus int
	}{
		{
			name:       "admin scope passes any check",
			allowed:    []string{"admin:*", "runner:*", "read:*"},
			tokenScope: "admin:*",
			authType:   "api_token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "admin scope passes admin-only check",
			allowed:    []string{"admin:*"},
			tokenScope: "admin:*",
			authType:   "api_token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "runner scope passes runner check",
			allowed:    []string{"admin:*", "runner:*"},
			tokenScope: "runner:*",
			authType:   "api_token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "runner scope blocked from admin-only",
			allowed:    []string{"admin:*"},
			tokenScope: "runner:*",
			authType:   "api_token",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "read scope passes read check",
			allowed:    []string{"admin:*", "runner:*", "read:*"},
			tokenScope: "read:*",
			authType:   "api_token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "read scope blocked from runner-only",
			allowed:    []string{"admin:*", "runner:*"},
			tokenScope: "read:*",
			authType:   "api_token",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "read scope blocked from admin-only",
			allowed:    []string{"admin:*"},
			tokenScope: "read:*",
			authType:   "api_token",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "oauth token always passes scope check",
			allowed:    []string{"admin:*"},
			tokenScope: "read write",
			authType:   "oauth",
			wantStatus: http.StatusOK,
		},
		{
			name:       "no auth result passes through (let auth middleware handle)",
			allowed:    []string{"admin:*"},
			tokenScope: "",
			authType:   "", // empty means no auth result in context
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := RequireScope(tt.allowed...)
			handler := middleware(okHandler)

			req := httptest.NewRequest("GET", "/test", nil)

			// Set auth result in context if authType is non-empty
			if tt.authType != "" {
				auth := &AuthResult{
					Type:  tt.authType,
					Scope: tt.tokenScope,
				}
				ctx := context.WithValue(req.Context(), ctxAuthResult, auth)
				req = req.WithContext(ctx)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestBuildAuthResult_APITokenWithScope(t *testing.T) {
	tok := &storage.Token{Name: "runner-token", Token: "abc123", Scope: "runner:*"}
	result := buildAuthResult(tok)

	if result.Type != "api_token" {
		t.Errorf("type = %q, want %q", result.Type, "api_token")
	}
	if result.Scope != "runner:*" {
		t.Errorf("scope = %q, want %q", result.Scope, "runner:*")
	}
}

func TestScopeEnforcement_EndToEnd(t *testing.T) {
	// Test the full auth + scope middleware chain
	validator := newScopedTokenValidator()
	validator.addToken("admin-token", "admin:*")
	validator.addToken("runner-token", "runner:*")
	validator.addToken("read-token", "read:*")

	cfg := config.Config{
		BrainDir:   "/tmp/test-brain",
		Port:       3000,
		Host:       "0.0.0.0",
		EnableAuth: true,
		CORSOrigin: "*",
		LogLevel:   "info",
	}

	router := NewRouter(cfg, WithTokenValidator(validator))
	srv := httptest.NewServer(router)
	defer srv.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		wantStatus int
	}{
		// Admin token — full access
		{
			name:       "admin can read entries",
			method:     "GET",
			path:       "/api/v1/entries",
			token:      "admin-token",
			wantStatus: http.StatusNotImplemented, // 501 because no handler, but passes auth + scope
		},
		{
			name:       "admin can list tokens",
			method:     "GET",
			path:       "/api/v1/tokens",
			token:      "admin-token",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "admin can read stats",
			method:     "GET",
			path:       "/api/v1/stats",
			token:      "admin-token",
			wantStatus: http.StatusNotImplemented,
		},

		// Runner token — can read tasks and claim/release
		{
			name:       "runner can read tasks",
			method:     "GET",
			path:       "/api/v1/stats",
			token:      "runner-token",
			wantStatus: http.StatusNotImplemented, // passes auth + scope
		},
		{
			name:       "runner blocked from token management",
			method:     "GET",
			path:       "/api/v1/tokens",
			token:      "runner-token",
			wantStatus: http.StatusForbidden,
		},

		// Read token — read-only
		{
			name:       "read can access stats",
			method:     "GET",
			path:       "/api/v1/stats",
			token:      "read-token",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "read can access entries list",
			method:     "GET",
			path:       "/api/v1/entries",
			token:      "read-token",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "read blocked from token management",
			method:     "GET",
			path:       "/api/v1/tokens",
			token:      "read-token",
			wantStatus: http.StatusForbidden,
		},

		// No token — 401
		{
			name:       "no token gets 401",
			method:     "GET",
			path:       "/api/v1/stats",
			token:      "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, srv.URL+tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SecureHeaders HSTS tests
// ---------------------------------------------------------------------------

func TestSecureHeaders_HSTS(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		wantHSTS bool
	}{
		{
			name:     "plain HTTP - no HSTS",
			headers:  nil,
			wantHSTS: false,
		},
		{
			name:     "behind TLS proxy - HSTS present",
			headers:  map[string]string{"X-Forwarded-Proto": "https"},
			wantHSTS: true,
		},
		{
			name:     "X-Forwarded-Ssl on - HSTS present",
			headers:  map[string]string{"X-Forwarded-Ssl": "on"},
			wantHSTS: true,
		},
		{
			name:     "Front-End-Https on - HSTS present",
			headers:  map[string]string{"Front-End-Https": "on"},
			wantHSTS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := SecureHeaders(okHandler)

			req := httptest.NewRequest("GET", "/test", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			// Always-present security headers
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Error("missing X-Content-Type-Options header")
			}
			if rec.Header().Get("X-Frame-Options") != "DENY" {
				t.Error("missing X-Frame-Options header")
			}

			hsts := rec.Header().Get("Strict-Transport-Security")
			if tt.wantHSTS {
				want := "max-age=31536000; includeSubDomains"
				if hsts != want {
					t.Errorf("Strict-Transport-Security = %q, want %q", hsts, want)
				}
			} else {
				if hsts != "" {
					t.Errorf("Strict-Transport-Security = %q, want empty (plain HTTP)", hsts)
				}
			}
		})
	}
}
