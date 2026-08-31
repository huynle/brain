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
		r.Get("/health", HealthHandler(cfg, o.embeddingReady))

		// Token bootstrap — unauthenticated (only works when zero tokens exist)
		// Solves the chicken-and-egg problem: need a token to create a token.
		if o.handler != nil && o.handler.tokens != nil {
			r.Post("/tokens/bootstrap", o.handler.HandleBootstrapToken)
		}

		// Password login — unauthenticated (credentials/refresh token ARE the
		// auth). The handlers themselves 404 when password login is not enabled.
		if o.handler != nil {
			r.Post("/auth/login", o.handler.HandleAuthLogin)
			r.Post("/auth/refresh", o.handler.HandleAuthRefresh)
			r.Post("/auth/logout", o.handler.HandleAuthLogout)
		}

		// All routes below require auth when enabled
		r.Group(func(r chi.Router) {
			r.Use(Auth(cfg.EnableAuth, o.validator, cfg.JWTSecret))
			// Record each authenticated request (with its actor) for the
			// global server-request log shown in the Logs tab. Installed after
			// Auth so the actor is present in context.
			r.Use(RequestRecorder)

			// ─── Server request log (read:* scope) ─────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				r.Get("/server/requests/recent", o.handler.HandleRecentRequests)
			})

			// ─── Attachments ───────────────────────────────────────
			r.Route("/attachments", func(r chi.Router) {
				// Attachment read operations — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.attachments != nil {
						r.Get("/", o.handler.HandleListAttachments)
						r.Get("/{attachmentID}", o.handler.HandleGetAttachment)
						r.Get("/{attachmentID}/content", o.handler.HandleDownloadAttachment)
						r.Get("/{attachmentID}/text", o.handler.HandleGetAttachmentText)
					} else {
						r.Get("/", notImplemented)
						r.Get("/{attachmentID}", notImplemented)
						r.Get("/{attachmentID}/content", notImplemented)
						r.Get("/{attachmentID}/text", notImplemented)
					}
				})

				// Attachment write operations — admin:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*"))
					if o.handler != nil && o.handler.attachments != nil {
						r.Post("/", o.handler.HandleCreateAttachment)
						r.Post("/backfill/extraction", o.handler.HandleBackfillAttachmentExtraction)
						r.Post("/{attachmentID}/extract", o.handler.HandleExtractAttachment)
						r.Delete("/{attachmentID}", o.handler.HandleDeleteAttachment)
					} else {
						r.Post("/", notImplemented)
						r.Post("/backfill/extraction", notImplemented)
						r.Post("/{attachmentID}/extract", notImplemented)
						r.Delete("/{attachmentID}", notImplemented)
					}
				})
			})

			// ─── Config (read:* scope) ───────────────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				r.Get("/config/task-defaults", TaskDefaultsHandler(cfg.TaskDefaults))
				if o.configHandler != nil {
					r.Get("/config", o.configHandler.HandleGet)
					r.Get("/config/schema", o.configHandler.HandleGetSchema)
				}
			})

			// ─── Config write (admin:* scope) ────────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*"))
				if o.configHandler != nil {
					r.Put("/config", o.configHandler.HandlePut)
				}
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

			// ─── Embeddings (admin:* scope) ─────────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*"))
				if o.handler != nil {
					r.Post("/embeddings/backfill", o.handler.HandleEmbeddingBackfill)
				} else {
					r.Post("/embeddings/backfill", notImplemented)
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

					// Attachment routes (must be before wildcard /{id})
					if o.handler != nil && o.handler.attachments != nil {
						r.Get("/{id}/attachments", o.handler.HandleListEntryAttachments)
					} else {
						r.Get("/{id}/attachments", notImplemented)
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
						r.Post("/bulk-delete", o.handler.HandleBulkDelete)
						if o.handler.attachments != nil {
							r.Post("/{id}/attachments", o.handler.HandleAttachEntryAttachment)
							r.Delete("/{id}/attachments/{attachmentID}", o.handler.HandleDetachEntryAttachment)
						} else {
							r.Post("/{id}/attachments", notImplemented)
							r.Delete("/{id}/attachments/{attachmentID}", notImplemented)
						}
						r.Post("/*", o.handler.HandlePostWildcard)
						r.Patch("/*", o.handler.HandleUpdateOrMetadata)
						r.Delete("/*", o.handler.HandleDeleteEntry)
					} else {
						r.Post("/", notImplemented)
						r.Post("/bulk-update", notImplemented)
						r.Post("/bulk-delete", notImplemented)
						r.Post("/*", notImplemented)
						r.Patch("/*", notImplemented)
						r.Delete("/*", notImplemented)
					}
				})
			})

			// ─── Events ──────────────────────────────────────────
			r.Route("/events", func(r chi.Router) {
				if o.handler != nil && o.handler.events != nil {
					r.Post("/", o.handler.HandleIngestEvents)
					r.Get("/stream", o.handler.HandleEventStream)
					r.Get("/recent", o.handler.HandleRecentEvents)
				} else {
					r.Post("/", notImplemented)
					r.Get("/stream", notImplemented)
					r.Get("/recent", notImplemented)
				}
			})

			// ─── Automation Runs ─────────────────────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				if o.handler != nil {
					r.Get("/automation-runs", o.handler.HandleListAutomationRuns)
					r.Get("/automation-runs/{runId}", o.handler.HandleGetAutomationRun)
				} else {
					r.Get("/automation-runs", notImplemented)
					r.Get("/automation-runs/{runId}", notImplemented)
				}
			})

			// ─── Automations (manual run) ────────────────────────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*"))
				if o.handler != nil {
					r.Post("/automations/run", o.handler.HandleRunAutomation)
				} else {
					r.Post("/automations/run", notImplemented)
				}
			})

			// ─── Assistant ───────────────────────────────────────
			r.Route("/assistant", func(r chi.Router) {
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.assistant != nil {
						r.Get("/status", o.handler.HandleAssistantStatus)
					} else {
						r.Get("/status", notImplemented)
					}
				})
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*"))
					if o.handler != nil && o.handler.assistant != nil {
						r.Post("/chat", o.handler.HandleAssistantChat)
						r.Post("/chat/stream", o.handler.HandleAssistantChatStream)
						r.Post("/goal-draft", o.handler.HandleAssistantGoalDraft)
					} else {
						r.Post("/chat", notImplemented)
						r.Post("/chat/stream", notImplemented)
						r.Post("/goal-draft", notImplemented)
					}
				})
			})

			// ─── Goals ───────────────────────────────────────────
			r.Route("/goals", func(r chi.Router) {
				// Goal read operations — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.goalService != nil {
						r.Get("/", o.handler.HandleListGoals)
						r.Get("/{goalId}/progress", o.handler.HandleGoalProgress)
						r.Get("/{goalId}/audit", o.handler.HandleGoalAudit)
					} else {
						r.Get("/", notImplemented)
						r.Get("/{goalId}/progress", notImplemented)
						r.Get("/{goalId}/audit", notImplemented)
					}
				})

				// Goal write operations — admin:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*"))
					if o.handler != nil && o.handler.goalService != nil {
						r.Post("/", o.handler.HandleCreateGoal)
						r.Patch("/{goalId}", o.handler.HandleUpdateGoal)
						r.Delete("/{goalId}", o.handler.HandleDeleteGoal)
						r.Post("/{goalId}/run", o.handler.HandleRunGoal)
					} else {
						r.Post("/", notImplemented)
						r.Patch("/{goalId}", notImplemented)
						r.Delete("/{goalId}", notImplemented)
						r.Post("/{goalId}/run", notImplemented)
					}
				})
			})

			// ─── Scheduler ────────────────────────────────────────
			r.Route("/scheduler", func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				if o.handler != nil && o.handler.scheduler != nil {
					r.Get("/status", o.handler.HandleSchedulerStatus)
				} else {
					r.Get("/status", notImplemented)
				}
			})

			// ─── Projects ──────────────────────────────────────────
			r.Route("/projects/{projectId}/placement", func(r chi.Router) {
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.placement != nil {
						r.Get("/", o.handler.HandleGetProjectPlacement)
					} else {
						r.Get("/", notImplemented)
					}
				})
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*"))
					if o.handler != nil && o.handler.placement != nil {
						r.Put("/", o.handler.HandlePutProjectPlacement)
					} else {
						r.Put("/", notImplemented)
					}
				})
			})

			// ─── Tasks ───────────────────────────────────────────
			r.Route("/tasks", func(r chi.Router) {
				// Runner-scoped dispatch protocol — runner:* scope.
				// Keep before /{projectId} so "runners" is not parsed as a project ID.
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*"))
					if o.handler != nil && o.handler.tasks != nil {
						r.Post("/runners/{runnerId}/dispatch/ack", o.handler.HandleAckDispatch)
						r.Post("/runners/{runnerId}/dispatch/reject", o.handler.HandleRejectDispatch)
						r.Post("/runners/{runnerId}/dispatch/release", o.handler.HandleReleaseDispatch)
					} else {
						r.Post("/runners/{runnerId}/dispatch/ack", notImplemented)
						r.Post("/runners/{runnerId}/dispatch/reject", notImplemented)
					}
				})

				// Task read operations — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.tasks != nil {
						r.Get("/", o.handler.HandleListProjects)
						// Multi-project SSE: /tasks/stream?projects=a,b,c or
						// ?projects=all. Registered at the /tasks level so it
						// sits BEFORE /{projectId}/stream in the tree — Chi
						// prefers the static "stream" segment over the
						// {projectId} placeholder anyway, but keeping them in
						// clearly different scopes makes the intent obvious.
						r.Get("/stream", o.handler.HandleMultiSSEStream)
					} else {
						r.Get("/", notImplemented)
						r.Get("/stream", notImplemented)
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
						r.Post("/runner/automations/pause/{projectId}", o.handler.HandlePauseProjectAutomations)
						r.Post("/runner/automations/resume/{projectId}", o.handler.HandleResumeProjectAutomations)
						r.Post("/runner/automations/pause", o.handler.HandlePauseAutomations)
						r.Post("/runner/automations/resume", o.handler.HandleResumeAutomations)
					} else {
						r.Post("/runner/pause/{projectId}", notImplemented)
						r.Post("/runner/resume/{projectId}", notImplemented)
						r.Post("/runner/pause", notImplemented)
						r.Post("/runner/resume", notImplemented)
						r.Post("/runner/automations/pause/{projectId}", notImplemented)
						r.Post("/runner/automations/resume/{projectId}", notImplemented)
						r.Post("/runner/automations/pause", notImplemented)
						r.Post("/runner/automations/resume", notImplemented)
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
							// Read-only: chain membership is derived, so
							// listing it needs no write authority.
							r.Get("/chains", o.handler.HandleListDependentChains)
							r.Get("/features/ready", o.handler.HandleGetReadyFeatures)
							r.Get("/features/{featureId}", o.handler.HandleGetFeature)

							// SSE stream
							r.Get("/stream", o.handler.HandleSSEStream)

							// Single task by ID (read)
							r.Get("/{taskId}", o.handler.HandleGetTask)

							// Claim status (read)
							r.Get("/{taskId}/claim-status", o.handler.HandleGetClaimStatus)

							// Task metadata (read)
							r.Get("/{taskId}/metadata", o.handler.HandleGetTaskMetadata)
						} else {
							r.Get("/", notImplemented)
							r.Get("/ready", notImplemented)
							r.Get("/waiting", notImplemented)
							r.Get("/blocked", notImplemented)
							r.Get("/next", notImplemented)
							r.Post("/status", notImplemented)

							r.Get("/features", notImplemented)
							r.Get("/chains", notImplemented)
							r.Get("/features/ready", notImplemented)
							r.Get("/features/{featureId}", notImplemented)

							r.Get("/stream", notImplemented)

							r.Get("/{taskId}", notImplemented)
							r.Get("/{taskId}/claim-status", notImplemented)
							r.Get("/{taskId}/metadata", notImplemented)
						}
					})

					// Scheduler visibility (read)
					r.Group(func(r chi.Router) {
						r.Use(RequireScope("admin:*", "runner:*", "read:*"))
						if o.handler != nil && o.handler.schedulerViews != nil {
							r.Get("/{taskId}/dispatch-lease", o.handler.HandleGetDispatchLease)
							r.Get("/{taskId}/placement-reasons", o.handler.HandleListPlacementReasons)
						} else {
							r.Get("/{taskId}/dispatch-lease", notImplemented)
							r.Get("/{taskId}/placement-reasons", notImplemented)
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
							r.Put("/features/{featureId}/assignment", o.handler.HandleAssignFeatureToRunner)
							r.Post("/features/{featureId}/assignment/clear", o.handler.HandleClearFeatureAssignment)
							r.Post("/features/{featureId}/run", o.handler.HandleRunFeature)
							r.Delete("/features/{featureId}/run", o.handler.HandleCancelDependentChain)
							r.Post("/features/{featureId}/resume", o.handler.HandleResumeFeature)
							// Project-level fanout: run every ready feature in this project.
							// Distinct from /features/{featureId}/run — no featureId path segment.
							r.Post("/run", o.handler.HandleRunProject)
							r.Post("/{taskId}/trigger", o.handler.HandleTriggerTask)
							r.Post("/{taskId}/dispatch", o.handler.HandleDispatchTask)
							r.Post("/{taskId}/run", o.handler.HandleRunTask)
							r.Post("/{taskId}/resume", o.handler.HandleResumeTask)
							// Project wipe. Lives on the tasks tree because
							// that is where a project is addressed by name,
							// but it erases every entry type, not just tasks.
							r.Delete("/", o.handler.HandleDeleteProject)
						} else {
							r.Post("/features/{featureId}/checkout", notImplemented)
							r.Put("/features/{featureId}/assignment", notImplemented)
							r.Post("/features/{featureId}/assignment/clear", notImplemented)
							r.Post("/features/{featureId}/run", notImplemented)
							r.Delete("/features/{featureId}/run", notImplemented)
							r.Post("/features/{featureId}/resume", notImplemented)
							r.Post("/run", notImplemented)
							r.Post("/{taskId}/trigger", notImplemented)
							r.Post("/{taskId}/dispatch", notImplemented)
							r.Post("/{taskId}/run", notImplemented)
							r.Post("/{taskId}/resume", notImplemented)
							r.Delete("/", notImplemented)
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

			// ─── Client Context ─────────────────────────────────
			r.Route("/context", func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				if o.handler != nil && o.handler.clientContext != nil {
					r.Post("/resolve", o.handler.HandleResolveClientContext)
				} else {
					r.Post("/resolve", notImplemented)
				}
			})

			// ─── Webhooks ────────────────────────────────────────
			r.Route("/webhooks", func(r chi.Router) {
				if o.handler != nil && o.handler.webhooks != nil {
					r.Post("/", o.handler.HandleCreateWebhook)
					r.Get("/", o.handler.HandleListWebhooks)
					r.Get("/{id}", o.handler.HandleGetWebhook)
					r.Patch("/{id}", o.handler.HandleUpdateWebhook)
					r.Delete("/{id}", o.handler.HandleDeleteWebhook)
					r.Get("/{id}/deliveries", o.handler.HandleListWebhookDeliveries)
					r.Post("/{id}/test", o.handler.HandleTestWebhook)
				} else {
					r.Post("/", notImplemented)
					r.Get("/", notImplemented)
					r.Get("/{id}", notImplemented)
					r.Patch("/{id}", notImplemented)
					r.Delete("/{id}", notImplemented)
					r.Get("/{id}/deliveries", notImplemented)
					r.Post("/{id}/test", notImplemented)
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
						r.Put("/{runnerId}/affinity", o.handler.HandleUpdateAffinity)
						r.Put("/{runnerId}/pause", o.handler.HandlePauseRunner)
						r.Put("/{runnerId}/resume", o.handler.HandleResumeRunner)
						r.Put("/{runnerId}/shutdown", o.handler.HandleShutdownRunner)
						r.Patch("/{runnerId}/config", o.handler.HandleUpdateRunnerConfig)
						r.Put("/{runnerId}/instances/{instanceId}", o.handler.HandleUpsertInstance)
						r.Delete("/{runnerId}/instances/{instanceId}", o.handler.HandleDeleteInstance)
						r.Get("/{runnerId}/bridge", o.handler.HandleRunnerBridge)
					} else {
						r.Post("/register", notImplemented)
						r.Post("/{runnerId}/heartbeat", notImplemented)
						r.Post("/{runnerId}/deregister", notImplemented)
						r.Put("/{runnerId}/affinity", notImplemented)
						r.Put("/{runnerId}/pause", notImplemented)
						r.Put("/{runnerId}/resume", notImplemented)
						r.Put("/{runnerId}/shutdown", notImplemented)
						r.Patch("/{runnerId}/config", notImplemented)
					}
				})

				// Runner read — read:* scope
				r.Group(func(r chi.Router) {
					r.Use(RequireScope("admin:*", "runner:*", "read:*"))
					if o.handler != nil && o.handler.runnerRegistry != nil {
						r.Get("/", o.handler.HandleListRunners)
						r.Get("/{runnerId}", o.handler.HandleGetRunner)
						r.Get("/{runnerId}/stream", o.handler.HandleRunnerStream)
						r.Get("/{runnerId}/instances", o.handler.HandleListRunnerInstances)
					} else {
						r.Get("/", notImplemented)
						r.Get("/{runnerId}", notImplemented)
						r.Get("/{runnerId}/stream", notImplemented)
						r.Get("/{runnerId}/instances", notImplemented)
					}
				})
			})

			// ─── OpenCode Instances (cross-runner overview) ──────
			r.Group(func(r chi.Router) {
				r.Use(RequireScope("admin:*", "runner:*", "read:*"))
				if o.handler != nil && o.handler.runnerRegistry != nil {
					r.Get("/instances", o.handler.HandleListAllInstances)
				} else {
					r.Get("/instances", notImplemented)
				}
			})

			// ─── Remote Control (control:* scope) ────────────────
			// Browser-facing surface for attaching to and spawning OpenCode
			// instances. control:* is code execution on runner machines —
			// never grant it implicitly (see oauthScopeSatisfies).
			// NOTE: control routes require ONLY control:* — admin:* passes via
			// RequireScope's wildcard early-return. Listing admin:* here would
			// let the legacy OAuth "mcp" grant (which satisfies any non-control
			// scope) leak into remote control.
			r.Route("/control", func(r chi.Router) {
				r.Use(RequireScope(ScopeControl))
				if o.handler != nil && o.handler.bridge != nil {
					// Session history is instance-independent: a completed
					// session is served by ID from any connected runner that
					// holds its on-disk storage.
					r.Get("/runners/{runnerId}/sessions/{sessionId}/history",
						o.handler.HandleControlSessionHistory)
					r.Post("/runners/{runnerId}/tasks/{taskId}/abort", o.handler.HandleControlAbortTask)
					// Runner shell: an unrestricted command on the runner
					// host, streamed back as SSE. control:* already implies
					// code execution there (spawn/prompt), so the scope on
					// this subtree is the whole authorization story.
					r.Post("/runners/{runnerId}/exec", o.handler.HandleControlExec)
					r.Post("/runners/{runnerId}/exec/{execId}/signal", o.handler.HandleControlExecSignal)
					r.Route("/runners/{runnerId}/instances", func(r chi.Router) {
						r.Post("/", o.handler.HandleControlSpawn)
						r.Route("/{instanceId}", func(r chi.Router) {
							r.Delete("/", o.handler.HandleControlKill)
							r.Get("/sessions", o.handler.HandleControlListSessions)
							r.Post("/sessions", o.handler.HandleControlCreateSession)
							r.Get("/sessions/status", o.handler.HandleControlSessionStatus)
							r.Get("/sessions/{sessionId}/messages", o.handler.HandleControlListMessages)
							r.Post("/sessions/{sessionId}/prompt", o.handler.HandleControlPrompt)
							r.Post("/sessions/{sessionId}/abort", o.handler.HandleControlAbort)
							r.Post("/sessions/{sessionId}/permissions/{permissionId}", o.handler.HandleControlPermission)
							r.Get("/permissions", o.handler.HandleControlPendingPermissions)
							r.Get("/events", o.handler.HandleControlEvents)
							r.Get("/agents", o.handler.HandleControlAgents)
							r.Get("/providers", o.handler.HandleControlProviders)
						})
					})
				} else {
					r.HandleFunc("/*", notImplemented)
				}
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
	handler        *Handler
	validator      TokenValidator
	rateLimiter    *RateLimiter
	embeddingReady bool
	configHandler  *ConfigHandler
}

// WithHandler returns a router option that wires the given Handler.
func WithHandler(h *Handler) func(*routerOptions) {
	return func(o *routerOptions) {
		o.handler = h
	}
}

// WithConfigHandler wires the read/write config endpoints
// (GET/PUT /api/v1/config, GET /api/v1/config/schema).
func WithConfigHandler(ch *ConfigHandler) func(*routerOptions) {
	return func(o *routerOptions) {
		o.configHandler = ch
	}
}

// WithTokenValidator returns a router option that sets the TokenValidator
// used by the Auth middleware. For dual-auth (API token + OAuth fallback),
// use WithTokenValidator instead.
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

// WithEmbeddingReady reports whether the configured embedding client is usable.
func WithEmbeddingReady(ready bool) func(*routerOptions) {
	return func(o *routerOptions) {
		o.embeddingReady = ready
	}
}
