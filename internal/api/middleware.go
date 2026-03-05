package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/huynle/brain-api/internal/config"
)

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
		next.ServeHTTP(w, r)
	})
}

// Auth returns middleware that validates Bearer tokens or ?token= query params.
// When enabled is false, all requests pass through.
func Auth(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.EnableAuth {
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

			// Validate against configured API key
			if token != cfg.APIKey {
				w.Header().Set("WWW-Authenticate", `Bearer realm="brain-api", error="invalid_token"`)
				WriteError(w, http.StatusUnauthorized,
					"Unauthorized",
					"Invalid authentication token",
				)
				return
			}

			next.ServeHTTP(w, r)
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
