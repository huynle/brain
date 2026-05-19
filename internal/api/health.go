package api

import (
	"net/http"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

// HealthHandler returns the health check endpoint handler.
// GET /api/v1/health → {"status": "healthy", "timestamp": "..."}
func HealthHandler(cfg config.Config, embeddingReady bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		embeddingStatus := "disabled"
		if cfg.Embedding.Enabled {
			embeddingStatus = "unavailable"
			if embeddingReady {
				embeddingStatus = "ready"
			}
		}
		resp := types.HealthResponse{
			Status:    "healthy",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Embedding: types.EmbeddingHealthStatus{
				Enabled:  cfg.Embedding.Enabled,
				Status:   embeddingStatus,
				Provider: cfg.Embedding.Provider,
				Model:    cfg.Embedding.Model,
			},
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}
