package oauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/huynle/brain-api/internal/storage"
)

// ---------------------------------------------------------------------------
// Context keys
// ---------------------------------------------------------------------------

type contextKey string

const (
	ctxOAuthClient contextKey = "oauthClientId"
	ctxOAuthScope  contextKey = "oauthScope"
)

// OAuthClientFromContext returns the OAuth client ID from the request context.
func OAuthClientFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxOAuthClient).(string)
	return v, ok
}

// OAuthScopeFromContext returns the OAuth scope string from the request context.
func OAuthScopeFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxOAuthScope).(string)
	return v, ok
}

// ---------------------------------------------------------------------------
// Token validator interface
// ---------------------------------------------------------------------------

// AccessTokenValidator validates OAuth access tokens against a backing store.
type AccessTokenValidator interface {
	GetAccessToken(ctx context.Context, tokenValue string) (*storage.OAuthAccessToken, error)
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// BearerAuth returns middleware that requires a valid OAuth Bearer token.
// It extracts the token from the Authorization header, validates it against
// the token store, and sets oauthClientId and oauthScope in the request context.
// Returns 401 if the token is missing or invalid.
func BearerAuth(validator AccessTokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r.Header.Get("Authorization"))
			if token == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="brain-api"`)
				writeOAuthError(w, http.StatusUnauthorized, "Unauthorized", "Missing authentication token")
				return
			}

			accessToken, err := validator.GetAccessToken(r.Context(), token)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="brain-api", error="invalid_token"`)
				writeOAuthError(w, http.StatusUnauthorized, "Unauthorized", "Invalid or expired access token")
				return
			}

			ctx := context.WithValue(r.Context(), ctxOAuthClient, accessToken.ClientID)
			ctx = context.WithValue(ctx, ctxOAuthScope, accessToken.Scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalBearerAuth returns middleware that validates an OAuth Bearer token
// if present, but passes through unauthenticated requests. If a token is
// provided and invalid, the request is rejected with 401.
func OptionalBearerAuth(validator AccessTokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r.Header.Get("Authorization"))
			if token == "" {
				// No token provided — pass through without auth context
				next.ServeHTTP(w, r)
				return
			}

			accessToken, err := validator.GetAccessToken(r.Context(), token)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="brain-api", error="invalid_token"`)
				writeOAuthError(w, http.StatusUnauthorized, "Unauthorized", "Invalid or expired access token")
				return
			}

			ctx := context.WithValue(r.Context(), ctxOAuthClient, accessToken.ClientID)
			ctx = context.WithValue(ctx, ctxOAuthScope, accessToken.Scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope returns middleware that checks the authenticated token has all
// required scopes. The "mcp" scope grants access to all "mcp:*" sub-scopes.
// Returns 403 Forbidden if any required scope is missing.
// Must be used after BearerAuth or OptionalBearerAuth (when token is present).
func RequireScope(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			grantedStr, ok := OAuthScopeFromContext(r.Context())
			if !ok {
				writeOAuthError(w, http.StatusForbidden, "Forbidden", "No OAuth scope in context")
				return
			}

			granted := parseScopeSet(grantedStr)

			for _, required := range scopes {
				if !hasScope(granted, required) {
					writeOAuthError(w, http.StatusForbidden, "Forbidden",
						"Insufficient scope: requires "+required)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ConditionalAuth returns BearerAuth when enabled is true, or a pass-through
// when disabled. Useful for toggling OAuth on MCP routes via configuration.
func ConditionalAuth(enabled bool, validator AccessTokenValidator) func(http.Handler) http.Handler {
	if enabled {
		return BearerAuth(validator)
	}
	return func(next http.Handler) http.Handler {
		return next
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractBearer extracts the token from a "Bearer <token>" Authorization header.
func extractBearer(authHeader string) string {
	if strings.HasPrefix(authHeader, "Bearer ") {
		return authHeader[7:]
	}
	if strings.HasPrefix(authHeader, "bearer ") {
		return authHeader[7:]
	}
	return ""
}

// parseScopeSet splits a space-delimited scope string into a set.
func parseScopeSet(scopeStr string) map[string]bool {
	set := make(map[string]bool)
	for _, s := range strings.Fields(scopeStr) {
		set[s] = true
	}
	return set
}

// hasScope checks if a required scope is satisfied by the granted set.
// The "mcp" scope implicitly grants access to all "mcp:*" sub-scopes.
func hasScope(granted map[string]bool, required string) bool {
	if granted[required] {
		return true
	}
	// "mcp" parent scope implies all "mcp:*" sub-scopes
	if strings.HasPrefix(required, "mcp:") && granted["mcp"] {
		return true
	}
	return false
}

// Note: writeOAuthError is defined in routes.go (same package).
// It writes {"error": errCode, "error_description": desc} per RFC 6749.
