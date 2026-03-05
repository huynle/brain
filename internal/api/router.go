// Package api implements the Brain API HTTP handlers and routing.
package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/config"
)

// NewRouter creates the chi router with all routes and middleware.
// An optional Handler can be provided to wire implemented endpoints;
// nil means all entry/task routes return 501 Not Implemented.
func NewRouter(cfg config.Config, opts ...func(*routerOptions)) *chi.Mux {
	var o routerOptions
	for _, fn := range opts {
		fn(&o)
	}

	r := chi.NewRouter()

	// Global middleware (applied to ALL routes)
	r.Use(Recovery)
	r.Use(SecureHeaders)
	r.Use(CORS(cfg))
	r.Use(RequestID)
	r.Use(Logger)

	// Custom 404 and 405 handlers
	r.NotFound(NotFoundHandler())
	r.MethodNotAllowed(MethodNotAllowedHandler())

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check — unauthenticated (before auth middleware)
		r.Get("/health", HealthHandler())

		// All routes below require auth when enabled
		r.Group(func(r chi.Router) {
			r.Use(Auth(cfg))

			// ─── Health & Stats ──────────────────────────────────
			r.Get("/stats", notImplemented)
			r.Get("/orphans", notImplemented)
			r.Get("/stale", notImplemented)
			r.Post("/link", notImplemented)

			// ─── Search ──────────────────────────────────────────
			r.Post("/search", notImplemented)
			r.Post("/inject", notImplemented)

			// ─── Entries CRUD ─────────────────────────────────────
			r.Route("/entries", func(r chi.Router) {
				if o.handler != nil {
					r.Post("/", o.handler.HandleCreateEntry)
					r.Get("/", o.handler.HandleListEntries)
				} else {
					r.Post("/", notImplemented)
					r.Get("/", notImplemented)
				}

				// Section routes (must be before wildcard /{id})
				r.Get("/{id}/sections", notImplemented)
				r.Get("/{id}/sections/{title}", notImplemented)

				// Graph routes (must be before wildcard /{id})
				r.Get("/{id}/backlinks", notImplemented)
				r.Get("/{id}/outlinks", notImplemented)
				r.Get("/{id}/related", notImplemented)

				// Verify route
				r.Post("/{id}/verify", notImplemented)

				// Move route
				if o.handler != nil {
					r.Post("/{id}/move", o.handler.HandleMoveEntry)
				} else {
					r.Post("/{id}/move", notImplemented)
				}

				// Entry CRUD by ID (wildcard — must be last)
				if o.handler != nil {
					r.Get("/{id}", o.handler.HandleGetEntry)
					r.Patch("/{id}", o.handler.HandleUpdateEntry)
					r.Delete("/{id}", o.handler.HandleDeleteEntry)
				} else {
					r.Get("/{id}", notImplemented)
					r.Patch("/{id}", notImplemented)
					r.Delete("/{id}", notImplemented)
				}
			})

			// ─── Tasks ───────────────────────────────────────────
			r.Route("/tasks", func(r chi.Router) {
				r.Get("/", notImplemented) // List projects

				r.Route("/{projectId}", func(r chi.Router) {
					r.Get("/", notImplemented)        // List tasks
					r.Get("/ready", notImplemented)   // Ready tasks
					r.Get("/waiting", notImplemented) // Waiting tasks
					r.Get("/blocked", notImplemented) // Blocked tasks
					r.Get("/next", notImplemented)    // Next task
					r.Post("/status", notImplemented) // Task status polling

					// Features
					r.Get("/features", notImplemented)
					r.Get("/features/ready", notImplemented)
					r.Get("/features/{featureId}", notImplemented)
					r.Post("/features/{featureId}/checkout", notImplemented)

					// SSE stream
					r.Get("/stream", notImplemented)

					// Per-task operations
					r.Post("/{taskId}/claim", notImplemented)
					r.Post("/{taskId}/release", notImplemented)
					r.Get("/{taskId}/claim-status", notImplemented)
					r.Post("/{taskId}/trigger", notImplemented)
				})
			})

			// ─── Monitors ────────────────────────────────────────
			r.Route("/monitors", func(r chi.Router) {
				r.Get("/templates", notImplemented)
				r.Get("/", notImplemented)
				r.Post("/", notImplemented)
				r.Patch("/{taskId}/toggle", notImplemented)
				r.Delete("/{taskId}", notImplemented)
			})
		})
	})

	return r
}

// routerOptions holds optional dependencies for the router.
type routerOptions struct {
	handler *Handler
}

// WithHandler returns a router option that wires the given Handler.
func WithHandler(h *Handler) func(*routerOptions) {
	return func(o *routerOptions) {
		o.handler = h
	}
}
