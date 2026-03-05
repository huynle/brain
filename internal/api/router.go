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
			if o.handler != nil {
				r.Get("/stats", o.handler.HandleGetStats)
				r.Get("/orphans", o.handler.HandleGetOrphans)
				r.Get("/stale", o.handler.HandleGetStale)
				r.Post("/link", o.handler.HandleGenerateLink)
			} else {
				r.Get("/stats", notImplemented)
				r.Get("/orphans", notImplemented)
				r.Get("/stale", notImplemented)
				r.Post("/link", notImplemented)
			}

			// ─── Search ──────────────────────────────────────────
			if o.handler != nil {
				r.Post("/search", o.handler.HandleSearch)
				r.Post("/inject", o.handler.HandleInject)
			} else {
				r.Post("/search", notImplemented)
				r.Post("/inject", notImplemented)
			}

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
				if o.handler != nil {
					r.Get("/{id}/sections", o.handler.HandleGetSections)
					r.Get("/{id}/sections/{title}", o.handler.HandleGetSection)
				} else {
					r.Get("/{id}/sections", notImplemented)
					r.Get("/{id}/sections/{title}", notImplemented)
				}

				// Graph routes (must be before wildcard /{id})
				if o.handler != nil {
					r.Get("/{id}/backlinks", o.handler.HandleGetBacklinks)
					r.Get("/{id}/outlinks", o.handler.HandleGetOutlinks)
					r.Get("/{id}/related", o.handler.HandleGetRelated)
				} else {
					r.Get("/{id}/backlinks", notImplemented)
					r.Get("/{id}/outlinks", notImplemented)
					r.Get("/{id}/related", notImplemented)
				}

				// Verify route
				if o.handler != nil {
					r.Post("/{id}/verify", o.handler.HandleVerifyEntry)
				} else {
					r.Post("/{id}/verify", notImplemented)
				}

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
				if o.handler != nil && o.handler.tasks != nil {
					r.Get("/", o.handler.HandleListProjects)
				} else {
					r.Get("/", notImplemented)
				}

				// Runner routes (must be before {projectId} wildcard)
				if o.handler != nil && o.handler.runner != nil {
					r.Post("/runner/pause/{projectId}", o.handler.HandlePauseProject)
					r.Post("/runner/resume/{projectId}", o.handler.HandleResumeProject)
					r.Post("/runner/pause", o.handler.HandlePauseAll)
					r.Post("/runner/resume", o.handler.HandleResumeAll)
					r.Get("/runner/status", o.handler.HandleRunnerStatus)
				} else {
					r.Post("/runner/pause/{projectId}", notImplemented)
					r.Post("/runner/resume/{projectId}", notImplemented)
					r.Post("/runner/pause", notImplemented)
					r.Post("/runner/resume", notImplemented)
					r.Get("/runner/status", notImplemented)
				}

				r.Route("/{projectId}", func(r chi.Router) {
					if o.handler != nil && o.handler.tasks != nil {
						r.Get("/", o.handler.HandleGetTasks)
						r.Get("/ready", o.handler.HandleGetReady)
						r.Get("/waiting", o.handler.HandleGetWaiting)
						r.Get("/blocked", o.handler.HandleGetBlocked)
						r.Get("/next", o.handler.HandleGetNext)
						r.Post("/status", o.handler.HandleMultiTaskStatus)

						// Features
						r.Get("/features", o.handler.HandleGetFeatures)
						r.Get("/features/ready", o.handler.HandleGetReadyFeatures)
						r.Get("/features/{featureId}", o.handler.HandleGetFeature)
						r.Post("/features/{featureId}/checkout", o.handler.HandleCheckoutFeature)

						// SSE stream
						r.Get("/stream", o.handler.HandleSSEStream)

						// Per-task operations
						r.Post("/{taskId}/claim", o.handler.HandleClaimTask)
						r.Post("/{taskId}/release", o.handler.HandleReleaseTask)
						r.Get("/{taskId}/claim-status", o.handler.HandleGetClaimStatus)
						r.Post("/{taskId}/trigger", o.handler.HandleTriggerTask)
					} else {
						r.Get("/", notImplemented)
						r.Get("/ready", notImplemented)
						r.Get("/waiting", notImplemented)
						r.Get("/blocked", notImplemented)
						r.Get("/next", notImplemented)
						r.Post("/status", notImplemented)

						r.Get("/features", notImplemented)
						r.Get("/features/ready", notImplemented)
						r.Get("/features/{featureId}", notImplemented)
						r.Post("/features/{featureId}/checkout", notImplemented)

						r.Get("/stream", notImplemented)

						r.Post("/{taskId}/claim", notImplemented)
						r.Post("/{taskId}/release", notImplemented)
						r.Get("/{taskId}/claim-status", notImplemented)
						r.Post("/{taskId}/trigger", notImplemented)
					}
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
