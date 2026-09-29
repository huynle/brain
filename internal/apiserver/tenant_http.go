package apiserver

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
	"net/http"
)

// tenantHTTPFactory is the shared composition seam for staged-29 HTTP fixtures
// and eventual trusted multi-mode composition. It neither opens/migrates storage
// nor provisions roots. Production buildHTTPHandler remains single-only until
// the atomic schema and P6/P7 gates are satisfied. No identity/operator adapter
// is supplied to these graphs.
func tenantHTTPFactory(stores func(tenant.ID) (*storage.TenantStore, error), roots *tenantfs.Resolver, cfg config.Config) graphFactory {
	cfg = copyGraphConfig(cfg)
	return func(ctx context.Context, id tenant.ID) (graphResource, error) {
		if stores == nil {
			return nil, errGraphUnavailable
		}
		store, err := stores(id)
		if err != nil {
			return nil, err
		}
		if store == nil || store.TenantID() != id {
			return nil, errGraphUnavailable
		}
		g, err := newTenantGraph(ctx, store, roots, cfg, graphIdentity{})
		if err != nil {
			return nil, err
		}
		return &tenantHTTPGraph{Handler: tenantContentRoutes(g.handler), graph: g}, nil
	}
}

type tenantHTTPGraph struct {
	http.Handler
	graph *tenantGraph
}

func (g *tenantHTTPGraph) Close() { g.graph.Close() }

// tenantWorkloadHTTP selects once from a TRUSTED context established upstream.
// It is a structural data-plane boundary, NOT authentication: tenant.Into carries
// scope only. P4 fixtures bind it explicitly. Do not mount behind legacy auth or
// header parsing in multi mode; P6 must authorize before this seam. There is no
// singleton fallback. The lease covers the entire synchronous handler/stream;
// hijacked/detached work is deliberately not exposed by tenantContentRoutes.
func tenantWorkloadHTTP(m *tenantGraphManager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := tenant.From(r.Context())
		if !ok {
			http.Error(w, "trusted tenant context required", http.StatusUnauthorized)
			return
		}
		if m == nil {
			http.Error(w, "tenant workload unavailable", http.StatusServiceUnavailable)
			return
		}
		lease, err := m.Acquire(r.Context(), id)
		if err != nil {
			http.Error(w, "tenant workload unavailable", http.StatusServiceUnavailable)
			return
		}
		defer lease.Release()
		lease.Graph.ServeHTTP(w, r.WithContext(lease.Context))
	})
}

// Explicitly reviewed migrated read-only surface. Never mount api.NewRouter
// here: it includes installation auth/config and unmigrated task/control planes.
// Writes (including generic entry writes which may create tasks), paid provider
// backfill/extraction, events, runners, sessions, OAuth and MCP remain sealed.
// Extend this allowlist only with receiver/side-effect and capability evidence.
// Single mode continues to use the complete, unchanged api.NewRouter.
func tenantContentRoutes(h *api.Handler) http.Handler {
	r := chi.NewRouter()
	unsupported := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "surface unavailable in tenant workload graph", http.StatusNotImplemented)
	}
	r.NotFound(unsupported)
	r.MethodNotAllowed(unsupported)
	r.Use(api.RequireScope("admin:*", "runner:*", "read:*"))
	r.Get("/api/v1/stats", h.HandleGetStats)
	r.Get("/api/v1/orphans", h.HandleGetOrphans)
	r.Get("/api/v1/stale", h.HandleGetStale)
	r.Post("/api/v1/search", h.HandleSearch)
	r.Post("/api/v1/inject", h.HandleInject)
	r.Route("/api/v1/entries", func(r chi.Router) {
		r.Get("/", h.HandleListEntries)
		r.Get("/{id}/attachments", h.HandleListEntryAttachments)
		r.Get("/{id}/sections", h.HandleGetSections)
		r.Get("/{id}/sections/{title}", h.HandleGetSection)
		r.Get("/{id}/backlinks", h.HandleGetBacklinks)
		r.Get("/{id}/outlinks", h.HandleGetOutlinks)
		r.Get("/{id}/related", h.HandleGetRelated)
		r.Get("/*", h.HandleGetEntry)
	})
	r.Route("/api/v1/attachments", func(r chi.Router) {
		r.Get("/", h.HandleListAttachments)
		r.Get("/{attachmentID}", h.HandleGetAttachment)
		r.Get("/{attachmentID}/content", h.HandleDownloadAttachment)
		r.Get("/{attachmentID}/text", h.HandleGetAttachmentText)
	})
	return r
}
