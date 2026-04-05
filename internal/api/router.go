// Package api implements the Brain API HTTP handlers and routing.
package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/config"
)

// RouterOption is a functional option for configuring the router.
type RouterOption = func(*routerOptions)

// NewRouter creates the chi router with all routes and middleware.
// An optional Handler can be provided to wire implemented endpoints;
// nil means all entry/task routes return 501 Not Implemented.
func NewRouter(cfg config.Config, opts ...RouterOption) *chi.Mux {
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

	// Rate limiting (applied after recovery/logging so 429s are logged)
	if o.rateLimiter != nil {
		r.Use(o.rateLimiter.Middleware())
	}

	// Custom 404 and 405 handlers
	r.NotFound(NotFoundHandler())
	r.MethodNotAllowed(MethodNotAllowedHandler())

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check — unauthenticated (before auth middleware)
		r.Get("/health", HealthHandler())

		// Token bootstrap — unauthenticated (only works when zero tokens exist)
		// Solves the chicken-and-egg problem: need a token to create a token.
		if o.handler != nil && o.handler.tokens != nil {
			r.Post("/tokens/bootstrap", o.handler.HandleBootstrapToken)
		}

		// All routes below require auth when enabled
		r.Group(func(r chi.Router) {
			r.Use(Auth(cfg.EnableAuth, o.validator))

			// ─── Config (read:* scope) ───────────────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				r.Get("/config/task-defaults", TaskDefaultsHandler(cfg.TaskDefaults))
			})

			// ─── Health & Stats (read:* scope) ──────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				if o.handler != nil {
					r.Get("/stats", o.handler.HandleGetStats)
					r.Get("/orphans", o.handler.HandleGetOrphans)
					r.Get("/stale", o.handler.HandleGetStale)
				} else {
					r.Get("/stats", notImplemented)
					r.Get("/orphans", notImplemented)
					r.Get("/stale", notImplemented)
				}
			})

			// ─── Link generation (admin:* scope) ────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*"))
				if o.handler != nil {
					r.Post("/link", o.handler.HandleGenerateLink)
				} else {
					r.Post("/link", notImplemented)
				}
			})

			// ─── Search (read:* scope) ──────────────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				if o.handler != nil {
					r.Post("/search", o.handler.HandleSearch)
					r.Post("/inject", o.handler.HandleInject)
				} else {
					r.Post("/search", notImplemented)
					r.Post("/inject", notImplemented)
				}
			})

			// ─── Entries CRUD ─────────────────────────────────────
			r.Route("/entries", func(r chi.Router) {
				// Read operations — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil {
						r.Get("/", o.handler.HandleListEntries)
					} else {
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

					// GET by ID/path (wildcard — must be last)
					if o.handler != nil {
						r.Get("/*", o.handler.HandleGetEntry)
					} else {
						r.Get("/*", notImplemented)
					}
				})

				// Write operations — admin:* scope only
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*"))
					if o.handler != nil {
						r.Post("/", o.handler.HandleCreateEntry)
						r.Post("/bulk-update", o.handler.HandleBulkUpdate)
						r.Post("/*", o.handler.HandlePostWildcard)
						r.Patch("/*", o.handler.HandleUpdateOrMetadata)
						r.Delete("/*", o.handler.HandleDeleteEntry)
					} else {
						r.Post("/", notImplemented)
						r.Post("/bulk-update", notImplemented)
						r.Post("/*", notImplemented)
						r.Patch("/*", notImplemented)
						r.Delete("/*", notImplemented)
					}
				})
			})

			// ─── Tasks ───────────────────────────────────────────
			r.Route("/tasks", func(r chi.Router) {
				// Task read operations — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.tasks != nil {
						r.Get("/", o.handler.HandleListProjects)
					} else {
						r.Get("/", notImplemented)
					}
				})

				// Runner control (admin:* scope — pause/resume are admin ops)
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*"))
					if o.handler != nil && o.handler.runner != nil {
						r.Post("/runner/pause/{projectId}", o.handler.HandlePauseProject)
						r.Post("/runner/resume/{projectId}", o.handler.HandleResumeProject)
						r.Post("/runner/pause", o.handler.HandlePauseAll)
						r.Post("/runner/resume", o.handler.HandleResumeAll)
					} else {
						r.Post("/runner/pause/{projectId}", notImplemented)
						r.Post("/runner/resume/{projectId}", notImplemented)
						r.Post("/runner/pause", notImplemented)
						r.Post("/runner/resume", notImplemented)
					}
				})

				// Runner status read — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.runner != nil {
						r.Get("/runner/status", o.handler.HandleRunnerStatus)
					} else {
						r.Get("/runner/status", notImplemented)
					}
				})

				r.Route("/{projectId}", func(r chi.Router) {
					// Task read operations — read:* scope
					r.Group(func(r chi.Router) {
						r.Use(RequireScope("admin:*", "runner:*", "read:*"))
						if o.handler != nil && o.handler.tasks != nil {
							r.Get("/", o.handler.HandleGetTasks)
							r.Get("/ready", o.handler.HandleGetReady)
							r.Get("/waiting", o.handler.HandleGetWaiting)
							r.Get("/blocked", o.handler.HandleGetBlocked)
							r.Get("/next", o.handler.HandleGetNext)
							r.Post("/status", o.handler.HandleMultiTaskStatus)

							// Features (read)
							r.Get("/features", o.handler.HandleGetFeatures)
							r.Get("/features/ready", o.handler.HandleGetReadyFeatures)
							r.Get("/features/{featureId}", o.handler.HandleGetFeature)

							// SSE stream
							r.Get("/stream", o.handler.HandleSSEStream)

							// Claim status (read)
							r.Get("/{taskId}/claim-status", o.handler.HandleGetClaimStatus)
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

							r.Get("/stream", notImplemented)

							r.Get("/{taskId}/claim-status", notImplemented)
						}
					})

					// Runner operations — runner:* scope (claim/release/renew)
					r.Group(func(r chi.Router) {
						r.Use(RequireScope("admin:*", "runner:*"))
						if o.handler != nil && o.handler.tasks != nil {
							r.Post("/{taskId}/claim", o.handler.HandleClaimTask)
							r.Post("/{taskId}/release", o.handler.HandleReleaseTask)
							r.Post("/{taskId}/renew", o.handler.HandleRenewClaim)
						} else {
							r.Post("/{taskId}/claim", notImplemented)
							r.Post("/{taskId}/release", notImplemented)
							r.Post("/{taskId}/renew", notImplemented)
						}

						// Log ingestion — runner:* scope
						if o.handler != nil && o.handler.logBuffer != nil {
							r.Post("/{taskId}/logs", o.handler.HandleIngestLogs)
						} else {
							r.Post("/{taskId}/logs", notImplemented)
						}
					})

					// Log retrieval — read:* scope
					r.Group(func(r chi.Router) {
						r.Use(RequireScope("admin:*", "runner:*", "read:*"))
						if o.handler != nil && o.handler.logBuffer != nil {
							r.Get("/{taskId}/logs", o.handler.HandleGetLogs)
						} else {
							r.Get("/{taskId}/logs", notImplemented)
						}
					})

					// Admin task operations — admin:* scope
					r.Group(func(r chi.Router) {
						r.Use(RequireScope("admin:*"))
						if o.handler != nil && o.handler.tasks != nil {
							r.Post("/features/{featureId}/checkout", o.handler.HandleCheckoutFeature)
							r.Post("/{taskId}/trigger", o.handler.HandleTriggerTask)
						} else {
							r.Post("/features/{featureId}/checkout", notImplemented)
							r.Post("/{taskId}/trigger", notImplemented)
						}
					})
				})
			})

			// ─── Tokens (admin:* scope) ──────────────────────────
			r.Route("/tokens", func(r chi.Router) {
				r.Use(RequireScope("admin:*"))
				if o.handler != nil && o.handler.tokens != nil {
					r.Post("/", o.handler.HandleCreateToken)
					r.Get("/", o.handler.HandleListTokens)
					r.Delete("/{name}", o.handler.HandleRevokeToken)
				} else {
					r.Post("/", notImplemented)
					r.Get("/", notImplemented)
					r.Delete("/{name}", notImplemented)
				}
			})

			// ─── Runners (Registry) ─────────────────────────────
			r.Route("/runners", func(r chi.Router) {
				// Runner operations — runner:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*"))
					if o.handler != nil && o.handler.runnerRegistry != nil {
						r.Post("/register", o.handler.HandleRegisterRunner)
						r.Post("/{runnerId}/heartbeat", o.handler.HandleHeartbeat)
						r.Post("/{runnerId}/deregister", o.handler.HandleDeregisterRunner)
					} else {
						r.Post("/register", notImplemented)
						r.Post("/{runnerId}/heartbeat", notImplemented)
						r.Post("/{runnerId}/deregister", notImplemented)
					}
				})

				// Runner read — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.runnerRegistry != nil {
						r.Get("/", o.handler.HandleListRunners)
						r.Get("/{runnerId}/stream", o.handler.HandleRunnerStream)
					} else {
						r.Get("/", notImplemented)
						r.Get("/{runnerId}/stream", notImplemented)
					}
				})
			})

			// ─── Monitors ────────────────────────────────────────
			r.Route("/monitors", func(r chi.Router) {
				// Read operations — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.monitor != nil {
						r.Get("/templates", o.handler.HandleListMonitorTemplates)
						r.Get("/", o.handler.HandleListMonitors)
					} else {
						r.Get("/templates", notImplemented)
						r.Get("/", notImplemented)
					}
				})

				// Write operations — admin:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*"))
					if o.handler != nil && o.handler.monitor != nil {
						r.Post("/", o.handler.HandleCreateMonitor)
						r.Delete("/by-scope", o.handler.HandleDeleteMonitorByScope)
						r.Patch("/{taskId}/toggle", o.handler.HandleToggleMonitor)
						r.Delete("/{taskId}", o.handler.HandleDeleteMonitor)
					} else {
						r.Post("/", notImplemented)
						r.Delete("/by-scope", notImplemented)
						r.Patch("/{taskId}/toggle", notImplemented)
						r.Delete("/{taskId}", notImplemented)
					}
				})
			})
		})
	})

	return r
}

// routerOptions holds optional dependencies for the router.
type routerOptions struct {
	handler     *Handler
	validator   TokenValidator
	rateLimiter *RateLimiter
}

// WithHandler returns a router option that wires the given Handler.
func WithHandler(h *Handler) func(*routerOptions) {
	return func(o *routerOptions) {
		o.handler = h
	}
}

// WithTokenValidator returns a router option that sets the TokenValidator
// used by the Auth middleware. For dual-auth (API token + OAuth fallback),
// use WithDualAuth instead.
func WithTokenValidator(v TokenValidator) func(*routerOptions) {
	return func(o *routerOptions) {
		o.validator = v
	}
}

// WithDualAuth returns a router option that sets up a CompositeValidator
// that tries API token validation first, then falls back to OAuth.
func WithDualAuth(apiValidator TokenValidator, oauthValidator OAuthAccessTokenValidator) func(*routerOptions) {
	return func(o *routerOptions) {
		o.validator = &CompositeValidator{
			APIValidator:   apiValidator,
			OAuthValidator: oauthValidator,
		}
	}
}

// WithRateLimiter returns a router option that applies per-IP rate limiting.
func WithRateLimiter(rl *RateLimiter) func(*routerOptions) {
	return func(o *routerOptions) {
		o.rateLimiter = rl
	}
}
