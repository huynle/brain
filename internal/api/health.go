package api

import (
	"net/http"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// HealthHandler returns the health check endpoint handler.
// GET /api/v1/health → {"status": "healthy", "timestamp": "..."}
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := types.HealthResponse{
			Status:    "healthy",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}
