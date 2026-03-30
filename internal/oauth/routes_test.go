package oauth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// newTestServer creates an httptest.Server with OAuth routes registered.
func newTestServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	store := NewStore()
	handler := NewHandler(store)
	r := chi.NewRouter()
	RegisterRoutes(r, handler)
	return httptest.NewServer(r), store
}

// registerTestClient creates a client in the store for testing.
func registerTestClient(store *Store, redirectURI string) *Client {
	client := &Client{
		ClientID:      "brain_testclient0000000000000000",
		ClientSecret:  "testsecret",
		ClientName:    "Test App",
		RedirectURIs:  []string{redirectURI},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	}
	store.SaveClient(client)
	return client
}

func decodeJSONBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	return m
}

// ──────────────────────────────────────────────────────────────
//  Server Metadata
// ──────────────────────────────────────────────────────────────

func TestHandleServerMetadata(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeJSONBody(t, resp)

	// Check required fields
	checks := map[string]string{
		"issuer":                 srv.URL,
		"authorization_endpoint": srv.URL + "/authorize",
		"token_endpoint":         srv.URL + "/token",
		"registration_endpoint":  srv.URL + "/register",
	}
	for key, want := range checks {
		got, ok := body[key].(string)
		if !ok || got != want {
			t.Errorf("metadata[%q] = %q, want %q", key, got, want)
		}
	}

	// Check response_types_supported
	rts, ok := body["response_types_supported"].([]any)
	if !ok || len(rts) == 0 {
		t.Fatal("missing response_types_supported")
	}
	if rts[0] != "code" {
		t.Errorf("response_types_supported[0] = %v, want code", rts[0])
	}

	// Check code_challenge_methods_supported
	ccms, ok := body["code_challenge_methods_supported"].([]any)
	if !ok || len(ccms) == 0 {
		t.Fatal("missing code_challenge_methods_supported")
	}
	if ccms[0] != "S256" {
		t.Errorf("code_challenge_methods_supported[0] = %v, want S256", ccms[0])
	}
}

func TestHandleServerMetadata_ForwardedHeaders(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		wantIssuer string
	}{
		{
			name: "X-Forwarded-Proto and X-Forwarded-Host",
			headers: map[string]string{
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "brain.example.com",
			},
			wantIssuer: "https://brain.example.com",
		},
		{
			name: "X-Forwarded-Ssl on",
			headers: map[string]string{
				"X-Forwarded-Ssl":  "on",
				"X-Forwarded-Host": "brain.example.com",
			},
			wantIssuer: "https://brain.example.com",
		},
		{
			name: "Front-End-Https on",
			headers: map[string]string{
				"Front-End-Https":  "on",
				"X-Forwarded-Host": "brain.example.com",
			},
			wantIssuer: "https://brain.example.com",
		},
		{
			name: "X-Forwarded-Proto takes priority over X-Forwarded-Ssl",
			headers: map[string]string{
				"X-Forwarded-Proto": "http",
				"X-Forwarded-Ssl":   "on",
				"X-Forwarded-Host":  "brain.example.com",
			},
			wantIssuer: "http://brain.example.com",
		},
		{
			name:       "No proxy headers uses default host",
			headers:    map[string]string{},
			wantIssuer: "http://example.com",
		},
		{
			name: "Only X-Forwarded-Host without scheme headers",
			headers: map[string]string{
				"X-Forwarded-Host": "proxy.example.com",
			},
			wantIssuer: "http://proxy.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStore()
			handler := NewHandler(store)
			r := chi.NewRouter()
			RegisterRoutes(r, handler)

			req := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			var body map[string]any
			json.NewDecoder(w.Body).Decode(&body)

			issuer, _ := body["issuer"].(string)
			if issuer != tt.wantIssuer {
				t.Errorf("issuer = %q, want %q", issuer, tt.wantIssuer)
			}

			// Also verify endpoint URLs use the same base
			authEndpoint, _ := body["authorization_endpoint"].(string)
			if authEndpoint != tt.wantIssuer+"/authorize" {
				t.Errorf("authorization_endpoint = %q, want %q", authEndpoint, tt.wantIssuer+"/authorize")
			}
		})
	}
}

func TestHandleProtectedResourceMetadata_ForwardedHeaders(t *testing.T) {
	store := NewStore()
	handler := NewHandler(store)
	r := chi.NewRouter()
	RegisterRoutes(r, handler)

	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/mcp", nil)
	req.Header.Set("X-Forwarded-Ssl", "on")
	req.Header.Set("X-Forwarded-Host", "brain.example.com")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)

	resource, _ := body["resource"].(string)
	if resource != "https://brain.example.com" {
		t.Errorf("resource = %q, want https://brain.example.com", resource)
	}

	servers, ok := body["authorization_servers"].([]any)
	if !ok || len(servers) == 0 {
		t.Fatal("missing authorization_servers")
	}
	if servers[0] != "https://brain.example.com" {
		t.Errorf("authorization_servers[0] = %v, want https://brain.example.com", servers[0])
	}
}

// ──────────────────────────────────────────────────────────────
//  Protected Resource Metadata
// ──────────────────────────────────────────────────────────────

func TestHandleProtectedResourceMetadata(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/.well-known/oauth-protected-resource/mcp")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeJSONBody(t, resp)
	resource, _ := body["resource"].(string)
	if resource != srv.URL {
		t.Errorf("resource = %q, want %q", resource, srv.URL)
	}
}

// ──────────────────────────────────────────────────────────────
//  Dynamic Client Registration
// ──────────────────────────────────────────────────────────────

func TestHandleRegister_Success(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	body := `{
		"client_name": "My MCP Client",
		"redirect_uris": ["http://localhost:8080/callback"],
		"grant_types": ["authorization_code"],
		"response_types": ["code"]
	}`

	resp, err := http.Post(srv.URL+"/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	result := decodeJSONBody(t, resp)

	clientID, _ := result["client_id"].(string)
	if !strings.HasPrefix(clientID, "brain_") {
		t.Errorf("client_id should start with brain_, got %q", clientID)
	}
	if len(clientID) != 38 { // "brain_" (6) + 32 hex chars
		t.Errorf("client_id length = %d, want 38", len(clientID))
	}

	clientSecret, _ := result["client_secret"].(string)
	if len(clientSecret) != 64 {
		t.Errorf("client_secret length = %d, want 64", len(clientSecret))
	}
}

func TestHandleRegister_MissingRedirectURIs(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	body := `{"client_name": "Bad Client"}`
	resp, err := http.Post(srv.URL+"/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleRegister_InvalidRedirectURI(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	body := `{"redirect_uris": ["not-a-url"]}`
	resp, err := http.Post(srv.URL+"/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────
//  Consent Page (GET /authorize)
// ──────────────────────────────────────────────────────────────

func TestHandleAuthorizeGET_RendersConsentPage(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	u := srv.URL + "/authorize?" + url.Values{
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp"},
		"state":                 {"test-state"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}.Encode()

	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)

	if !strings.Contains(bodyStr, "Test App") {
		t.Error("consent page should contain client name")
	}
	if !strings.Contains(bodyStr, "mcp") {
		t.Error("consent page should contain scope name")
	}
	if !strings.Contains(bodyStr, "Allow") {
		t.Error("consent page should contain Allow button")
	}
	if !strings.Contains(bodyStr, "Deny") {
		t.Error("consent page should contain Deny button")
	}
}

func TestHandleAuthorizeGET_MissingClientID(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/authorize?redirect_uri=http://localhost/cb")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleAuthorizeGET_UnknownClient(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/authorize?client_id=unknown&redirect_uri=http://localhost/cb")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleAuthorizeGET_InvalidRedirectURI(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	resp, err := http.Get(srv.URL + "/authorize?client_id=" + client.ClientID + "&redirect_uri=http://evil.com/cb")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────
//  Consent Form (POST /authorize)
// ──────────────────────────────────────────────────────────────

func TestHandleAuthorizePOST_Allow(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	// Prevent following redirects
	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{
		"action":                {"allow"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"state":                 {"test-state"},
		"scope":                 {"mcp"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
	}

	resp, err := httpClient.PostForm(srv.URL+"/authorize", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}

	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("no Location header: %v", err)
	}

	code := loc.Query().Get("code")
	if code == "" {
		t.Error("redirect should contain code param")
	}
	if len(code) != 32 {
		t.Errorf("code length = %d, want 32", len(code))
	}

	state := loc.Query().Get("state")
	if state != "test-state" {
		t.Errorf("state = %q, want test-state", state)
	}
}

func TestHandleAuthorizePOST_Deny(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{
		"action":       {"deny"},
		"client_id":    {client.ClientID},
		"redirect_uri": {"http://localhost:8080/callback"},
		"state":        {"test-state"},
	}

	resp, err := httpClient.PostForm(srv.URL+"/authorize", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}

	loc, _ := resp.Location()
	if loc.Query().Get("error") != "access_denied" {
		t.Errorf("expected error=access_denied, got %q", loc.Query().Get("error"))
	}
}

func TestHandleAuthorizePOST_PINRequired(t *testing.T) {
	// Set PIN env var for this test
	t.Setenv("OAUTH_PIN", "1234")

	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Wrong PIN
	form := url.Values{
		"action":       {"allow"},
		"client_id":    {client.ClientID},
		"redirect_uri": {"http://localhost:8080/callback"},
		"scope":        {"mcp"},
		"pin":          {"wrong"},
	}

	resp, err := httpClient.PostForm(srv.URL+"/authorize", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for wrong PIN, got %d", resp.StatusCode)
	}

	// Correct PIN
	form.Set("pin", "1234")
	resp, err = httpClient.PostForm(srv.URL+"/authorize", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 for correct PIN, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────
//  Token Exchange (POST /token)
// ──────────────────────────────────────────────────────────────

func TestHandleToken_AuthCodeGrant(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	// Use RFC 7636 test vector
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := GenerateCodeChallenge(verifier)

	// Simulate authorization: save code directly
	store.SaveAuthCode(&AuthCode{
		Code:                "testcode123",
		ClientID:            client.ClientID,
		RedirectURI:         "http://localhost:8080/callback",
		Scope:               "mcp",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           expiresInFuture(),
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"testcode123"},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"client_id":     {client.ClientID},
		"code_verifier": {verifier},
	}

	resp, err := http.PostForm(srv.URL+"/token", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := decodeJSONBody(t, resp)

	if result["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", result["token_type"])
	}
	accessToken, _ := result["access_token"].(string)
	if len(accessToken) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("access_token length = %d, want 64", len(accessToken))
	}
	refreshToken, _ := result["refresh_token"].(string)
	if len(refreshToken) != 64 {
		t.Errorf("refresh_token length = %d, want 64", len(refreshToken))
	}
	if result["scope"] != "mcp" {
		t.Errorf("scope = %v, want mcp", result["scope"])
	}
}

func TestHandleToken_AuthCodeGrant_InvalidCode(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"nonexistent"},
	}

	resp, err := http.PostForm(srv.URL+"/token", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleToken_AuthCodeGrant_PKCEFails(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	store.SaveAuthCode(&AuthCode{
		Code:                "testcode-pkce",
		ClientID:            client.ClientID,
		RedirectURI:         "http://localhost:8080/callback",
		Scope:               "mcp",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		ExpiresAt:           expiresInFuture(),
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"testcode-pkce"},
		"client_id":     {client.ClientID},
		"code_verifier": {"wrong-verifier-that-is-long-enough-to-pass-format-check"},
	}

	resp, err := http.PostForm(srv.URL+"/token", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}

	result := decodeJSONBody(t, resp)
	if result["error"] != "invalid_grant" {
		t.Errorf("error = %v, want invalid_grant", result["error"])
	}
}

func TestHandleToken_AuthCodeGrant_MissingVerifier(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	store.SaveAuthCode(&AuthCode{
		Code:          "testcode-no-verifier",
		ClientID:      client.ClientID,
		RedirectURI:   "http://localhost:8080/callback",
		CodeChallenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		ExpiresAt:     expiresInFuture(),
	})

	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"testcode-no-verifier"},
		"client_id":  {client.ClientID},
	}

	resp, err := http.PostForm(srv.URL+"/token", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing code_verifier, got %d", resp.StatusCode)
	}
}

func TestHandleToken_AuthCodeGrant_CodeUsedOnce(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	store.SaveAuthCode(&AuthCode{
		Code:        "one-use-code",
		ClientID:    client.ClientID,
		RedirectURI: "http://localhost:8080/callback",
		ExpiresAt:   expiresInFuture(),
	})

	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"one-use-code"},
		"client_id":  {client.ClientID},
	}

	// First use: should succeed
	resp, err := http.PostForm(srv.URL+"/token", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first use should succeed, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Second use: should fail
	resp, err = http.PostForm(srv.URL+"/token", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("second use should fail, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandleToken_AuthCodeGrant_ClientSecretBasicAuth(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	store.SaveAuthCode(&AuthCode{
		Code:        "basic-auth-code",
		ClientID:    client.ClientID,
		RedirectURI: "http://localhost:8080/callback",
		ExpiresAt:   expiresInFuture(),
	})

	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"basic-auth-code"},
	}

	req, _ := http.NewRequest("POST", srv.URL+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(client.ClientID, client.ClientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 with Basic auth, got %d: %s", resp.StatusCode, string(body))
	}
	resp.Body.Close()
}

// ──────────────────────────────────────────────────────────────
//  Refresh Token
// ──────────────────────────────────────────────────────────────

func TestHandleToken_RefreshGrant(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	// Manually store a refresh token
	store.SaveRefreshToken("refresh-abc", &RefreshEntry{
		ClientID:  client.ClientID,
		Scope:     "mcp",
		ExpiresAt: expiresInFuture(),
	})

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"refresh-abc"},
		"client_id":     {client.ClientID},
	}

	resp, err := http.PostForm(srv.URL+"/token", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := decodeJSONBody(t, resp)

	if result["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", result["token_type"])
	}
	newRefresh, _ := result["refresh_token"].(string)
	if newRefresh == "refresh-abc" {
		t.Error("refresh token should be rotated (new value)")
	}
	if len(newRefresh) != 64 {
		t.Errorf("new refresh_token length = %d, want 64", len(newRefresh))
	}
}

func TestHandleToken_RefreshGrant_TokenConsumed(t *testing.T) {
	srv, store := newTestServer(t)
	defer srv.Close()

	client := registerTestClient(store, "http://localhost:8080/callback")

	store.SaveRefreshToken("refresh-once", &RefreshEntry{
		ClientID:  client.ClientID,
		Scope:     "mcp",
		ExpiresAt: expiresInFuture(),
	})

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"refresh-once"},
		"client_id":     {client.ClientID},
	}

	// First use: success
	resp, _ := http.PostForm(srv.URL+"/token", form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first refresh should succeed, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Second use: fail (token was consumed)
	resp, _ = http.PostForm(srv.URL+"/token", form)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("reused refresh token should fail, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandleToken_UnsupportedGrantType(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	form := url.Values{
		"grant_type": {"client_credentials"},
	}

	resp, err := http.PostForm(srv.URL+"/token", form)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}

	result := decodeJSONBody(t, resp)
	if result["error"] != "unsupported_grant_type" {
		t.Errorf("error = %v, want unsupported_grant_type", result["error"])
	}
}

// ──────────────────────────────────────────────────────────────
//  Full Authorization Code Flow (end-to-end)
// ──────────────────────────────────────────────────────────────

func TestFullAuthCodeFlow(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Step 1: Register client
	regBody := `{
		"client_name": "E2E Test Client",
		"redirect_uris": ["http://localhost:9999/callback"]
	}`
	resp, err := http.Post(srv.URL+"/register", "application/json", strings.NewReader(regBody))
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	regResult := decodeJSONBody(t, resp)
	clientID := regResult["client_id"].(string)

	// Step 2: Authorize (consent allow)
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := GenerateCodeChallenge(verifier)

	form := url.Values{
		"action":                {"allow"},
		"client_id":             {clientID},
		"redirect_uri":          {"http://localhost:9999/callback"},
		"scope":                 {"mcp"},
		"state":                 {"e2e-state"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
	}
	resp, err = httpClient.PostForm(srv.URL+"/authorize", form)
	if err != nil {
		t.Fatalf("authorize failed: %v", err)
	}
	loc, _ := resp.Location()
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("no code in redirect")
	}

	// Step 3: Exchange code for tokens
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost:9999/callback"},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	resp, err = http.PostForm(srv.URL+"/token", tokenForm)
	if err != nil {
		t.Fatalf("token exchange failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	tokenResult := decodeJSONBody(t, resp)
	refreshToken := tokenResult["refresh_token"].(string)

	// Step 4: Refresh token
	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}
	resp, err = http.PostForm(srv.URL+"/token", refreshForm)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("refresh returned %d: %s", resp.StatusCode, string(body))
	}

	refreshResult := decodeJSONBody(t, resp)
	newRefresh := refreshResult["refresh_token"].(string)
	if newRefresh == refreshToken {
		t.Error("refresh token should be rotated")
	}
}

// ──────────────────────────────────────────────────────────────
//  Templates
// ──────────────────────────────────────────────────────────────

func TestDescribeScopes(t *testing.T) {
	scopes := DescribeScopes([]string{"mcp", "mcp:read", "unknown"})
	if len(scopes) != 3 {
		t.Fatalf("expected 3 scopes, got %d", len(scopes))
	}
	if scopes[0].Name != "mcp" || scopes[0].Description != "Full access to the MCP server" {
		t.Errorf("scope 0: got %+v", scopes[0])
	}
	if scopes[2].Description != "Unknown scope" {
		t.Errorf("unknown scope should have 'Unknown scope' description, got %q", scopes[2].Description)
	}
}

// ──────────────────────────────────────────────────────────────
//  Helpers
// ──────────────────────────────────────────────────────────────

func expiresInFuture() time.Time {
	return time.Now().Add(10 * time.Minute)
}
