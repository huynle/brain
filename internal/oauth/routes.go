// Package oauth implements OAuth 2.1 authorization server endpoints
// with PKCE support for the Brain API.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// ──────────────────────────────────────────────────────────────
//  Types
// ──────────────────────────────────────────────────────────────

// Client represents a dynamically registered OAuth client.
type Client struct {
	ClientID      string    `json:"client_id"`
	ClientSecret  string    `json:"client_secret,omitempty"`
	ClientName    string    `json:"client_name,omitempty"`
	RedirectURIs  []string  `json:"redirect_uris"`
	GrantTypes    []string  `json:"grant_types"`
	ResponseTypes []string  `json:"response_types"`
	Scope         string    `json:"scope,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// AuthCode represents an issued authorization code.
type AuthCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           time.Time
}

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// RefreshEntry stores a refresh token's metadata.
type RefreshEntry struct {
	ClientID  string
	Scope     string
	ExpiresAt time.Time
}

// ──────────────────────────────────────────────────────────────
//  In-memory store
// ──────────────────────────────────────────────────────────────

// Store holds in-memory OAuth state. In a production system this
// would be backed by a database. Thread-safe via sync.RWMutex.
type Store struct {
	mu            sync.RWMutex
	clients       map[string]*Client       // keyed by client_id
	authCodes     map[string]*AuthCode     // keyed by code
	refreshTokens map[string]*RefreshEntry // keyed by token
}

// NewStore creates an empty OAuth store.
func NewStore() *Store {
	return &Store{
		clients:       make(map[string]*Client),
		authCodes:     make(map[string]*AuthCode),
		refreshTokens: make(map[string]*RefreshEntry),
	}
}

func (s *Store) GetClient(id string) (*Client, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clients[id]
	return c, ok
}

func (s *Store) SaveClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c.ClientID] = c
}

func (s *Store) SaveAuthCode(ac *AuthCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authCodes[ac.Code] = ac
}

func (s *Store) ConsumeAuthCode(code string) (*AuthCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, ok := s.authCodes[code]
	if ok {
		delete(s.authCodes, code)
	}
	return ac, ok
}

func (s *Store) SaveRefreshToken(token string, entry *RefreshEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshTokens[token] = entry
}

func (s *Store) ConsumeRefreshToken(token string) (*RefreshEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.refreshTokens[token]
	if ok {
		delete(s.refreshTokens, token)
	}
	return entry, ok
}

// ──────────────────────────────────────────────────────────────
//  Handler
// ──────────────────────────────────────────────────────────────

// AccessTokenStore persists OAuth access tokens so they survive across
// requests and can be validated by the auth middleware.
type AccessTokenStore interface {
	SaveAccessToken(ctx context.Context, token, clientID, scope string, expiresAt int64) error
}

// Handler serves the OAuth endpoints. It uses an in-memory Store
// for transient OAuth flow state (auth codes, clients) and an
// AccessTokenStore to persist issued access tokens.
type Handler struct {
	store            *Store
	accessTokenStore AccessTokenStore
}

// NewHandler creates an OAuth handler backed by the given store.
func NewHandler(store *Store, opts ...func(*Handler)) *Handler {
	h := &Handler{store: store}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithAccessTokenStore sets the persistent store for OAuth access tokens.
func WithAccessTokenStore(ats AccessTokenStore) func(*Handler) {
	return func(h *Handler) {
		h.accessTokenStore = ats
	}
}

// ──────────────────────────────────────────────────────────────
//  Helpers
// ──────────────────────────────────────────────────────────────

// issuerURL returns the external base URL derived from the request,
// respecting reverse proxy headers for scheme and host detection.
//
// Scheme detection priority:
//  1. X-Forwarded-Proto header (standard)
//  2. X-Forwarded-Ssl: on (common alternative)
//  3. Front-End-Https: on (Microsoft/legacy)
//  4. r.TLS (direct TLS connection)
//  5. Default: "http"
//
// Host detection priority:
//  1. X-Forwarded-Host header
//  2. r.Host (from Host header or request URL)
func issuerURL(r *http.Request) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.Header.Get("X-Forwarded-Ssl") == "on" || r.Header.Get("Front-End-Https") == "on" {
		scheme = "https"
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}

func writeOAuthError(w http.ResponseWriter, status int, errCode, desc string) {
	writeJSON(w, status, map[string]string{
		"error":             errCode,
		"error_description": desc,
	})
}

// oauthPIN returns the configured PIN or empty string if not set.
func oauthPIN() string {
	return os.Getenv("OAUTH_PIN")
}

// ──────────────────────────────────────────────────────────────
//  GET /.well-known/oauth-authorization-server
// ──────────────────────────────────────────────────────────────

// HandleServerMetadata returns the OAuth Authorization Server Metadata (RFC 8414).
func (h *Handler) HandleServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := issuerURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/token",
		"registration_endpoint":                 base + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic", "none"},
		"scopes_supported":                      []string{"mcp", "mcp:read", "mcp:write", "control"},
	})
}

// ──────────────────────────────────────────────────────────────
//  GET /.well-known/oauth-protected-resource/mcp
// ──────────────────────────────────────────────────────────────

// HandleProtectedResourceMetadata returns the Protected Resource Metadata (RFC 8707).
func (h *Handler) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := issuerURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base,
		"authorization_servers":    []string{base},
		"scopes_supported":         []string{"mcp", "mcp:read", "mcp:write", "control"},
		"bearer_methods_supported": []string{"header"},
	})
}

// ──────────────────────────────────────────────────────────────
//  POST /register
// ──────────────────────────────────────────────────────────────

// HandleRegister handles dynamic client registration (RFC 7591).
func (h *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientName    string   `json:"client_name"`
		RedirectURIs  []string `json:"redirect_uris"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
		Scope         string   `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON body")
		return
	}

	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "redirect_uris is required")
		return
	}

	// Validate redirect URIs
	for _, uri := range req.RedirectURIs {
		if uri == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "redirect_uris must not contain empty strings")
			return
		}
		if !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", fmt.Sprintf("invalid redirect_uri: %s", uri))
			return
		}
	}

	// Defaults
	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code"}
	}
	if len(req.ResponseTypes) == 0 {
		req.ResponseTypes = []string{"code"}
	}

	// Generate credentials
	idHex, err := randomHex(16) // 32 hex chars
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate client_id")
		return
	}
	secretHex, err := randomHex(32) // 64 hex chars
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate client_secret")
		return
	}

	client := &Client{
		ClientID:      "brain_" + idHex,
		ClientSecret:  secretHex,
		ClientName:    req.ClientName,
		RedirectURIs:  req.RedirectURIs,
		GrantTypes:    req.GrantTypes,
		ResponseTypes: req.ResponseTypes,
		Scope:         req.Scope,
		CreatedAt:     time.Now(),
	}

	h.store.SaveClient(client)

	writeJSON(w, http.StatusCreated, client)
}

// ──────────────────────────────────────────────────────────────
//  GET /authorize — consent page
// ──────────────────────────────────────────────────────────────

// HandleAuthorizeGET renders the HTML consent page.
func (h *Handler) HandleAuthorizeGET(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	scope := q.Get("scope")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	if clientID == "" || redirectURI == "" {
		http.Error(w, "missing client_id or redirect_uri", http.StatusBadRequest)
		return
	}

	client, ok := h.store.GetClient(clientID)
	if !ok {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}

	// Verify redirect_uri is registered
	uriValid := false
	for _, u := range client.RedirectURIs {
		if u == redirectURI {
			uriValid = true
			break
		}
	}
	if !uriValid {
		http.Error(w, "redirect_uri not registered for this client", http.StatusBadRequest)
		return
	}

	scopes := strings.Fields(scope)
	if len(scopes) == 0 {
		scopes = []string{"mcp"}
	}

	clientName := client.ClientName
	if clientName == "" {
		clientName = clientID
	}

	data := ConsentData{
		ClientName:          clientName,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		State:               state,
		RawScope:            scope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Scopes:              DescribeScopes(scopes),
		PINRequired:         oauthPIN() != "",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := consentTmpl.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// ──────────────────────────────────────────────────────────────
//  POST /authorize — handle consent form
// ──────────────────────────────────────────────────────────────

// HandleAuthorizePOST processes the consent form submission.
func (h *Handler) HandleAuthorizePOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	action := r.FormValue("action")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	scope := r.FormValue("scope")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")

	// Deny → redirect with error
	if action == "deny" {
		target := redirectURI + "?error=access_denied"
		if state != "" {
			target += "&state=" + state
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	// Validate client
	client, ok := h.store.GetClient(clientID)
	if !ok {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}

	// Verify redirect_uri
	uriValid := false
	for _, u := range client.RedirectURIs {
		if u == redirectURI {
			uriValid = true
			break
		}
	}
	if !uriValid {
		http.Error(w, "redirect_uri not registered", http.StatusBadRequest)
		return
	}

	// Check PIN if configured
	if pin := oauthPIN(); pin != "" {
		if r.FormValue("pin") != pin {
			// Re-render consent page with error
			scopes := strings.Fields(scope)
			if len(scopes) == 0 {
				scopes = []string{"mcp"}
			}
			clientName := client.ClientName
			if clientName == "" {
				clientName = clientID
			}
			data := ConsentData{
				ClientName:          clientName,
				ClientID:            clientID,
				RedirectURI:         redirectURI,
				State:               state,
				RawScope:            scope,
				CodeChallenge:       codeChallenge,
				CodeChallengeMethod: codeChallengeMethod,
				Scopes:              DescribeScopes(scopes),
				PINRequired:         true,
				Error:               "Invalid PIN",
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			consentTmpl.Execute(w, data) //nolint:errcheck
			return
		}
	}

	// Generate authorization code
	codeHex, err := randomHex(16) // 32 hex chars
	if err != nil {
		http.Error(w, "failed to generate code", http.StatusInternalServerError)
		return
	}

	ac := &AuthCode{
		Code:                codeHex,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           time.Now().Add(10 * time.Minute),
	}
	h.store.SaveAuthCode(ac)

	target := redirectURI + "?code=" + codeHex
	if state != "" {
		target += "&state=" + state
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// ──────────────────────────────────────────────────────────────
//  POST /token
// ──────────────────────────────────────────────────────────────

// HandleToken handles token exchange (authorization_code and refresh_token).
func (h *Handler) HandleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form data")
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.handleAuthCodeGrant(w, r)
	case "refresh_token":
		h.handleRefreshGrant(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type",
			fmt.Sprintf("grant_type %q is not supported", grantType))
	}
}

func (h *Handler) handleAuthCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	codeVerifier := r.FormValue("code_verifier")
	clientID, clientSecret, hasBasicAuth := r.BasicAuth()

	// Fallback to form-based client auth
	if !hasBasicAuth {
		clientID = r.FormValue("client_id")
		clientSecret = r.FormValue("client_secret")
	}

	if code == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code is required")
		return
	}

	ac, ok := h.store.ConsumeAuthCode(code)
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}

	// Check expiry
	if time.Now().After(ac.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code expired")
		return
	}

	// Verify client_id matches
	if clientID != "" && clientID != ac.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	clientID = ac.ClientID

	// Verify redirect_uri
	if redirectURI != "" && redirectURI != ac.RedirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}

	// Verify client secret if client is confidential
	client, ok := h.store.GetClient(clientID)
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client")
		return
	}
	if client.ClientSecret != "" && clientSecret != "" && clientSecret != client.ClientSecret {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
		return
	}

	// PKCE verification
	if ac.CodeChallenge != "" {
		if codeVerifier == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier is required")
			return
		}
		if err := ValidateCodeVerifier(codeVerifier, ac.CodeChallenge); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed: "+err.Error())
			return
		}
	}

	// Generate tokens
	accessToken, err := randomHex(32)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate access token")
		return
	}
	refreshToken, err := randomHex(32)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate refresh token")
		return
	}

	h.store.SaveRefreshToken(refreshToken, &RefreshEntry{
		ClientID:  clientID,
		Scope:     ac.Scope,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour), // 30 days
	})

	// Persist access token to SQLite so the auth middleware can validate it
	expiresIn := 3600
	if h.accessTokenStore != nil {
		expiresAt := time.Now().Unix() + int64(expiresIn)
		if err := h.accessTokenStore.SaveAccessToken(context.Background(), accessToken, clientID, ac.Scope, expiresAt); err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to persist access token")
			return
		}
	}

	writeJSON(w, http.StatusOK, TokenPair{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
		RefreshToken: refreshToken,
		Scope:        ac.Scope,
	})
}

func (h *Handler) handleRefreshGrant(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	clientID, clientSecret, hasBasicAuth := r.BasicAuth()
	if !hasBasicAuth {
		clientID = r.FormValue("client_id")
		clientSecret = r.FormValue("client_secret")
	}

	if refreshToken == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}

	entry, ok := h.store.ConsumeRefreshToken(refreshToken)
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired refresh token")
		return
	}

	if time.Now().After(entry.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token expired")
		return
	}

	// Verify client
	if clientID != "" && clientID != entry.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	client, ok := h.store.GetClient(entry.ClientID)
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client")
		return
	}
	if client.ClientSecret != "" && clientSecret != "" && clientSecret != client.ClientSecret {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
		return
	}

	// Rotate: issue new access + refresh tokens
	newAccess, err := randomHex(32)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate token")
		return
	}
	newRefresh, err := randomHex(32)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate token")
		return
	}

	h.store.SaveRefreshToken(newRefresh, &RefreshEntry{
		ClientID:  entry.ClientID,
		Scope:     entry.Scope,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	})

	// Persist new access token to SQLite
	if h.accessTokenStore != nil {
		expiresAt := time.Now().Unix() + 3600
		if err := h.accessTokenStore.SaveAccessToken(context.Background(), newAccess, entry.ClientID, entry.Scope, expiresAt); err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to persist access token")
			return
		}
	}

	writeJSON(w, http.StatusOK, TokenPair{
		AccessToken:  newAccess,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: newRefresh,
		Scope:        entry.Scope,
	})
}

// ──────────────────────────────────────────────────────────────
//  Route registration
// ──────────────────────────────────────────────────────────────

// RegisterRoutes registers all OAuth routes on the given chi router.
// These are top-level routes (not under /api/v1) per OAuth conventions.
func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/.well-known/oauth-authorization-server", h.HandleServerMetadata)
	r.Get("/.well-known/oauth-protected-resource", h.HandleProtectedResourceMetadata)
	r.Get("/.well-known/oauth-protected-resource/mcp", h.HandleProtectedResourceMetadata)
	r.Post("/register", h.HandleRegister)
	r.Get("/authorize", h.HandleAuthorizeGET)
	r.Post("/authorize", h.HandleAuthorizePOST)
	r.Post("/token", h.HandleToken)
}
