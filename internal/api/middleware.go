package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
)

// TokenValidator validates authentication tokens against a backing store.
type TokenValidator interface {
	ValidateToken(ctx context.Context, tokenValue string) (*storage.Token, error)
}

// AuthResult carries authentication metadata after successful validation.
// Downstream handlers read these values from the request context.
type AuthResult struct {
	Type     string // "api_token" or "oauth"
	Name     string // token name (api) or client_id (oauth)
	ClientID string // oauth only
	Scope    string // oauth only
}

// context keys for auth info
type authContextKey string

const (
	ctxAuthResult authContextKey = "authResult"
)

// AuthResultFromContext returns the AuthResult from the request context, if present.
func AuthResultFromContext(ctx context.Context) (*AuthResult, bool) {
	v, ok := ctx.Value(ctxAuthResult).(*AuthResult)
	return v, ok
}

// OAuthAccessTokenValidator validates OAuth access tokens against a backing store.
type OAuthAccessTokenValidator interface {
	GetAccessToken(ctx context.Context, tokenValue string) (*storage.OAuthAccessToken, error)
}

// CompositeValidator tries an API token validator first, then falls back to an
// OAuth access token validator. It records which validator succeeded in an AuthResult.
type CompositeValidator struct {
	APIValidator   TokenValidator
	OAuthValidator OAuthAccessTokenValidator
}

// ValidateToken tries the API token validator first; on failure it falls back
// to the OAuth validator. Returns a *storage.Token on success (for interface
// compat) and stashes the full AuthResult in the returned token's Name field
// encoded as a type prefix so the Auth middleware can reconstruct it.
func (cv *CompositeValidator) ValidateToken(ctx context.Context, tokenValue string) (*storage.Token, error) {
	// Try API token first
	if cv.APIValidator != nil {
		tok, err := cv.APIValidator.ValidateToken(ctx, tokenValue)
		if err == nil {
			return tok, nil
		}
	}

	// Fallback to OAuth
	if cv.OAuthValidator != nil {
		oauthTok, err := cv.OAuthValidator.GetAccessToken(ctx, tokenValue)
		if err == nil {
			// Wrap in storage.Token for interface compatibility.
			// Encode auth type in the Name field as "oauth:<clientID>" so Auth
			// middleware can parse it.
			return &storage.Token{
				Name:  "oauth:" + oauthTok.ClientID + ":" + oauthTok.Scope,
				Token: oauthTok.Token,
			}, nil
		}
	}

	return nil, fmt.Errorf("no validator accepted the token")
}

// RequestID generates a UUID and sets the X-Request-ID header on both
// the request context and the response.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.New().String()
		w.Header().Set("X-Request-ID", id)
		r.Header.Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

// tokenPattern matches token query parameters for sanitization.
var tokenPattern = regexp.MustCompile(`(^|[&])token=[^&\s]+`)

// Logger logs each request with method, path, status, and duration.
// Authorization header values and token query params are masked.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		duration := time.Since(start)
		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path += "?" + tokenPattern.ReplaceAllString(r.URL.RawQuery, "${1}token=***")
		}

		slog.Info("request",
			"method", r.Method,
			"path", path,
			"status", sw.status,
			"duration", duration.String(),
		)
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.wroteHeader {
		sw.status = code
		sw.wroteHeader = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.wroteHeader = true
	}
	return sw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher by delegating to the underlying ResponseWriter.
// This is required for SSE (Server-Sent Events) to work through the Logger middleware.
func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// CORS returns middleware that sets CORS headers based on config.
func CORS(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := cfg.CORSOrigin

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if origin != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Recovery catches panics and returns a 500 JSON error response.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				stack := debug.Stack()
				slog.Error("panic recovered",
					"error", fmt.Sprintf("%v", err),
					"stack", string(stack),
				)
				WriteError(w, http.StatusInternalServerError,
					"Internal Server Error",
					"An unexpected error occurred",
				)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecureHeaders sets security-related HTTP headers.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Add HSTS when behind TLS proxy
		if isTLS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

// isTLS returns true if the request was made over TLS, either directly
// or via a TLS-terminating reverse proxy.
func isTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	if r.Header.Get("X-Forwarded-Ssl") == "on" {
		return true
	}
	if r.Header.Get("Front-End-Https") == "on" {
		return true
	}
	return false
}

// Auth returns middleware that validates Bearer tokens or ?token= query params
// against a TokenValidator (e.g. database-backed token store).
// When enabled is false, all requests pass through.
func Auth(enabled bool, validator TokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from Authorization header or query param
			token := extractBearerToken(r.Header.Get("Authorization"))
			if token == "" {
				token = r.URL.Query().Get("token")
			}

			if token == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="brain-api"`)
				WriteError(w, http.StatusUnauthorized,
					"Unauthorized",
					"Missing authentication token",
				)
				return
			}

			validToken, err := validator.ValidateToken(r.Context(), token)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="brain-api", error="invalid_token"`)
				WriteError(w, http.StatusUnauthorized,
					"Unauthorized",
					"Invalid authentication token",
				)
				return
			}

			// Build AuthResult from the validated token
			authResult := buildAuthResult(validToken)
			ctx := context.WithValue(r.Context(), ctxAuthResult, authResult)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractBearerToken extracts the token from a "Bearer <token>" header value.
func extractBearerToken(authHeader string) string {
	if strings.HasPrefix(authHeader, "Bearer ") {
		return authHeader[7:]
	}
	if strings.HasPrefix(authHeader, "bearer ") {
		return authHeader[7:]
	}
	return ""
}

// buildAuthResult inspects the validated token and produces an AuthResult.
// If the token name starts with "oauth:" it was produced by CompositeValidator's
// OAuth fallback path; otherwise it's a plain API token.
func buildAuthResult(tok *storage.Token) *AuthResult {
	if strings.HasPrefix(tok.Name, "oauth:") {
		// Format: "oauth:<clientID>:<scope>"
		parts := strings.SplitN(tok.Name, ":", 3)
		clientID := ""
		scope := ""
		if len(parts) >= 2 {
			clientID = parts[1]
		}
		if len(parts) >= 3 {
			scope = parts[2]
		}
		return &AuthResult{
			Type:     "oauth",
			Name:     clientID,
			ClientID: clientID,
			Scope:    scope,
		}
	}
	return &AuthResult{
		Type:  "api_token",
		Name:  tok.Name,
		Scope: tok.Scope,
	}
}

// RequireScope returns middleware that checks the AuthResult scope against
// the required scopes. If the authenticated token's scope is not in the
// allowed list, it returns 403 Forbidden.
// Admin scope ("admin:*") always passes all scope checks.
func RequireScope(allowed ...string) func(http.Handler) http.Handler {
	allowedSet := make(map[string]bool, len(allowed))
	for _, s := range allowed {
		allowedSet[s] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth, ok := AuthResultFromContext(r.Context())
			if !ok {
				// No auth result — let the auth middleware handle 401
				next.ServeHTTP(w, r)
				return
			}

			// admin:* always passes
			if auth.Scope == "admin:*" {
				next.ServeHTTP(w, r)
				return
			}

			// OAuth tokens pass scope checks (they have their own scope system)
			if auth.Type == "oauth" {
				next.ServeHTTP(w, r)
				return
			}

			// Check if token scope is in the allowed set
			if allowedSet[auth.Scope] {
				next.ServeHTTP(w, r)
				return
			}

			WriteError(w, http.StatusForbidden, "Forbidden",
				fmt.Sprintf("Token scope %q insufficient; requires one of: %s",
					auth.Scope, scopeList(allowed)))
		})
	}
}

// scopeList formats a list of scopes for display.
func scopeList(scopes []string) string {
	if len(scopes) == 0 {
		return "(none)"
	}
	parts := make([]string, len(scopes))
	for i, s := range scopes {
		parts[i] = s
	}
	return strings.Join(parts, ", ")
}
