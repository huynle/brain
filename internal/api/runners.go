package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// HandleRegisterRunner handles POST /runners/register — register a new runner.
func (h *Handler) HandleRegisterRunner(w http.ResponseWriter, r *http.Request) {
	var req types.RunnerRegistration
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	if req.RunnerID == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "runner_id", Message: "runner_id is required"},
		})
		return
	}

	if req.Hostname == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "hostname", Message: "hostname is required"},
		})
		return
	}

	info, err := h.runnerRegistry.Register(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	slog.Info("runner registered",
		"runner_id", req.RunnerID,
		"hostname", req.Hostname,
		"max_parallel", req.MaxParallel,
	)

	WriteJSON(w, http.StatusOK, map[string]any{
		"registered":             true,
		"runner":                 info,
		"heartbeat_interval":     30,
		"lease_renewal_interval": 300,
	})
}

// HandleHeartbeat handles POST /runners/{runnerId}/heartbeat — update runner heartbeat.
func (h *Handler) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")

	var req types.RunnerHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	err := h.runnerRegistry.Heartbeat(r.Context(), runnerID, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "runner not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"ack":      true,
		"commands": []any{},
	})
}

// HandleDeregisterRunner handles POST /runners/{runnerId}/deregister — remove a runner.
func (h *Handler) HandleDeregisterRunner(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")

	err := h.runnerRegistry.Deregister(r.Context(), runnerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "runner not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	slog.Info("runner deregistered", "runner_id", runnerID)

	WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// HandleListRunners handles GET /runners — list all registered runners.
func (h *Handler) HandleListRunners(w http.ResponseWriter, r *http.Request) {
	resp, err := h.runnerRegistry.ListRunners(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}
