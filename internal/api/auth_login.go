package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/storage"
)

// CredentialVerifier verifies operator username/password credentials for the
// password login flow. *auth.Verifier satisfies it.
type CredentialVerifier interface {
	Verify(username, password string) bool
	Configured() bool
	Username() string
}

// PasswordTokenStore issues and rotates tokens for the password login flow.
// *storage.StorageLayer satisfies it.
type PasswordTokenStore interface {
	CreateAccessToken(ctx context.Context, token *storage.OAuthAccessToken) error
	CreateRefreshToken(ctx context.Context, token *storage.OAuthRefreshToken) error
	ConsumeRefreshToken(ctx context.Context, token string) (*storage.OAuthRefreshToken, error)
}

// Password login issues access/refresh tokens through the same persistent store
// the OAuth flow uses, so the auth middleware validates them identically. The
// synthetic client_id distinguishes operator password sessions from OAuth/MCP
// clients. The operator gets full scope.
const (
	passwordClientID  = "brain_password_session"
	passwordScope     = "mcp control"
	pwAccessTokenTTL  = time.Hour
	pwRefreshTokenTTL = 30 * 24 * time.Hour

	maxLoginFails = 5
	loginLockout  = 5 * time.Minute
)

// loginThrottle is a small per-IP failed-login limiter to slow brute force.
type loginThrottle struct {
	mu       sync.Mutex
	fails    map[string]int
	lockedAt map[string]time.Time
}

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{fails: map[string]int{}, lockedAt: map[string]time.Time{}}
}

func (t *loginThrottle) locked(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if at, ok := t.lockedAt[ip]; ok {
		if time.Since(at) < loginLockout {
			return true
		}
		delete(t.lockedAt, ip)
		delete(t.fails, ip)
	}
	return false
}

func (t *loginThrottle) fail(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fails[ip]++
	if t.fails[ip] >= maxLoginFails {
		t.lockedAt[ip] = time.Now()
	}
}

func (t *loginThrottle) reset(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.fails, ip)
	delete(t.lockedAt, ip)
}

// tokenResponse mirrors the OAuth token response so the PWA can reuse the same
// token-handling code path.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// HandleAuthLogin handles POST /api/v1/auth/login — username/password →
// access + refresh tokens. Public (no bearer token required).
func (h *Handler) HandleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if h.credentials == nil || !h.credentials.Configured() || h.passwordTokens == nil {
		WriteError(w, http.StatusNotFound, "Not Found", "password login is not enabled")
		return
	}
	ip := clientIP(r)
	if h.loginThrottle.locked(ip) {
		WriteError(w, http.StatusTooManyRequests, "Too Many Requests",
			"too many failed login attempts; try again later")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	if !h.credentials.Verify(req.Username, req.Password) {
		h.loginThrottle.fail(ip)
		WriteError(w, http.StatusUnauthorized, "Unauthorized", "invalid username or password")
		return
	}
	h.loginThrottle.reset(ip)
	h.issuePasswordTokens(w)
}

// HandleAuthRefresh handles POST /api/v1/auth/refresh — rotate a refresh token
// into a fresh access + refresh pair. Public (the refresh token is the credential).
func (h *Handler) HandleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	if h.passwordTokens == nil {
		WriteError(w, http.StatusNotFound, "Not Found", "password login is not enabled")
		return
	}
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "refresh_token is required")
		return
	}

	entry, err := h.passwordTokens.ConsumeRefreshToken(r.Context(), req.RefreshToken)
	if err != nil || entry == nil || entry.ClientID != passwordClientID {
		WriteError(w, http.StatusUnauthorized, "Unauthorized", "invalid or expired refresh token")
		return
	}
	h.issuePasswordTokens(w)
}

// HandleAuthLogout handles POST /api/v1/auth/logout — revoke the supplied
// refresh token. The access token expires on its own. Public; always 204.
func (h *Handler) HandleAuthLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.RefreshToken != "" && h.passwordTokens != nil {
		_, _ = h.passwordTokens.ConsumeRefreshToken(r.Context(), req.RefreshToken)
	}
	w.WriteHeader(http.StatusNoContent)
}

// issuePasswordTokens generates, persists, and writes an access + refresh token pair.
func (h *Handler) issuePasswordTokens(w http.ResponseWriter) {
	access, err := randomToken()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "failed to generate token")
		return
	}
	refresh, err := randomToken()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "failed to generate token")
		return
	}

	ctx := context.Background()
	now := time.Now()
	if err := h.passwordTokens.CreateAccessToken(ctx, &storage.OAuthAccessToken{
		Token:     access,
		ClientID:  passwordClientID,
		Scope:     passwordScope,
		ExpiresAt: now.Add(pwAccessTokenTTL).Unix(),
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "failed to persist token")
		return
	}
	if err := h.passwordTokens.CreateRefreshToken(ctx, &storage.OAuthRefreshToken{
		Token:     refresh,
		ClientID:  passwordClientID,
		Scope:     passwordScope,
		ExpiresAt: now.Add(pwRefreshTokenTTL).Unix(),
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "failed to persist token")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(pwAccessTokenTTL.Seconds()),
		RefreshToken: refresh,
		Scope:        passwordScope,
	})
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// clientIP extracts the best-effort client IP for rate-limiting, honoring
// X-Forwarded-For (set by the Traefik reverse proxy) then RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
