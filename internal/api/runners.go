package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// decorateInstances merges live bridge state (pending permission counts,
// connection-derived status) into instance records before they are returned.
// No-op when the bridge hub is not wired.
func (h *Handler) decorateInstances(instances []types.OpencodeInstance) {
	if h.bridge == nil {
		return
	}
	h.bridge.DecorateInstances(instances)
}

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

	// Publish runner_registered SSE event to lifecycle subscribers
	if h.hub != nil {
		h.hub.PublishRunnerRegistered(types.SSERunnerRegisteredData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventRunnerRegistered,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
			},
			RunnerID:    info.RunnerID,
			Hostname:    info.Hostname,
			Executors:   req.Executors,
			MaxParallel: req.MaxParallel,
			Labels:      req.Labels,
		})
	}

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

	// Publish runner_offline SSE event for deregistration
	if h.hub != nil {
		h.hub.PublishRunnerOffline(types.SSERunnerOfflineData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventRunnerOffline,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
			},
			RunnerID: runnerID,
			Status:   "offline",
			Reason:   "deregistered",
		})
	}

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
	// Decorate with live bridge connectivity so callers can tell which runners
	// can actually serve remote-control requests right now (distinct from the
	// heartbeat-derived online/stale status).
	if h.bridge != nil {
		for i := range resp.Runners {
			resp.Runners[i].BridgeConnected = h.bridge.Connected(resp.Runners[i].RunnerID)
		}
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleGetRunner handles GET /runners/{runnerId} — get a single runner by ID.
func (h *Handler) HandleGetRunner(w http.ResponseWriter, r *http.Request) {
	if h.runnerRegistry == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "runner registry not available")
		return
	}
	runnerID := chi.URLParam(r, "runnerId")
	runner, err := h.runnerRegistry.GetRunner(r.Context(), runnerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("runner %q not found", runnerID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, runner)
}

// HandleUpdateAffinity handles PUT /runners/{runnerId}/affinity — update runner feature affinity.
func (h *Handler) HandleUpdateAffinity(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")

	var req struct {
		FeatureIDs []string `json:"feature_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	err := h.runnerRegistry.UpdateAffinity(r.Context(), runnerID, req.FeatureIDs)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "runner not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	slog.Info("runner affinity updated",
		"runner_id", runnerID,
		"feature_ids", req.FeatureIDs,
	)

	// Push affinity change to runner via SSE command channel
	if h.hub != nil {
		h.hub.PublishRunnerCommand(runnerID, "affinity_updated", map[string]interface{}{
			"feature_ids": req.FeatureIDs,
		})
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// HandleRunnerStream handles GET /runners/{runnerId}/stream — runner-scoped SSE event stream.
// Carries task change events and server-pushed commands (affinity, config, dispatch, shutdown).
func (h *Handler) HandleRunnerStream(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")

	// Verify runner exists
	if h.runnerRegistry != nil {
		_, err := h.runnerRegistry.GetRunner(r.Context(), runnerID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				WriteError(w, http.StatusNotFound, "Not Found", "runner not found")
				return
			}
			WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
			return
		}
	}

	// Check that the ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Subscribe to hub using runner-scoped topic key
	ch, unsub := h.hub.Subscribe(realtime.RunnerTopic(runnerID))
	defer unsub()

	now := types.TimeNowUTC().Format(time.RFC3339)

	// Send connected event
	writeSSEEvent(w, "connected", types.RunnerSSEConnectedData{
		RunnerSSEEventData: types.RunnerSSEEventData{
			Type:      types.SSEEventConnected,
			Transport: "sse",
			Timestamp: now,
			RunnerID:  runnerID,
		},
	})
	flusher.Flush()

	// Start heartbeat ticker
	heartbeat := time.NewTicker(DefaultHeartbeatInterval)
	defer heartbeat.Stop()

	// Event loop
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			slog.Debug("runner SSE stream disconnected", "runner_id", runnerID)
			return

		case msg, ok := <-ch:
			if !ok {
				// Channel closed
				return
			}
			writeSSEEvent(w, msg.Event, msg.Data)
			flusher.Flush()

		case <-heartbeat.C:
			writeSSEEvent(w, "heartbeat", types.RunnerSSEEventData{
				Type:      types.SSEEventHeartbeat,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
				RunnerID:  runnerID,
			})
			flusher.Flush()
		}
	}
}

// HandleUpdateRunnerConfig handles PATCH /runners/{runnerId}/config — update runner configuration.
// Accepts {"maxParallel": N} and updates the runner record.
// Config changes are pushed to the runner via SSE command channel.
func (h *Handler) HandleUpdateRunnerConfig(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")

	var req struct {
		MaxParallel int `json:"maxParallel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	if req.MaxParallel <= 0 {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "maxParallel", Message: "maxParallel must be positive"},
		})
		return
	}

	if err := h.runnerRegistry.UpdateConfig(r.Context(), runnerID, req.MaxParallel); err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "runner not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	slog.Info("runner config updated",
		"runner_id", runnerID,
		"max_parallel", req.MaxParallel,
	)

	// Push config change to runner via SSE command channel
	if h.hub != nil {
		h.hub.PublishRunnerCommand(runnerID, "config_updated", map[string]interface{}{
			"maxParallel": req.MaxParallel,
		})
	}

	// Return updated runner info
	updated, _ := h.runnerRegistry.GetRunner(r.Context(), runnerID)
	WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"runner":  updated,
	})
}

// runnerPauseScopePayload is the SSE payload for runner-scoped pause and
// resume commands.
//
// The scope matters: without it the runner cannot tell a runner pause from a
// *global project* pause, files it under its per-project snapshot, and then
// wipes it on the next reconcile tick (that snapshot is replaced wholesale
// from GetRunnerStatus, which only ever reports project pauses). Runners see
// scope="runner" and route it to their own dial instead.
var runnerPauseScopePayload = map[string]any{"scope": "runner"}

// HandlePauseRunner handles PUT /runners/{runnerId}/pause — pause a runner.
//
// The pause is persisted first (runner_pause_state); the SSE command is only
// a fast path so a connected runner stops immediately. Persistence is what
// actually stops work: the scheduler skips paused runners when placing
// dispatch leases, and a runner that was offline, mid-reconnect, or restarted
// picks the pause up when it reconciles.
func (h *Handler) HandlePauseRunner(w http.ResponseWriter, r *http.Request) {
	h.setRunnerPaused(w, r, true)
}

// HandleResumeRunner handles PUT /runners/{runnerId}/resume — resume a runner.
func (h *Handler) HandleResumeRunner(w http.ResponseWriter, r *http.Request) {
	h.setRunnerPaused(w, r, false)
}

func (h *Handler) setRunnerPaused(w http.ResponseWriter, r *http.Request, paused bool) {
	runnerID := chi.URLParam(r, "runnerId")
	action := "resume"
	if paused {
		action = "pause"
	}

	if _, err := h.runnerRegistry.GetRunner(r.Context(), runnerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("runner %q not found", runnerID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	slog.Info("runner "+action+" requested", "runner_id", runnerID)

	// Persist before publishing. Reporting success for a pause that was
	// never recorded is what let a "paused" runner keep acking dispatches.
	if err := h.runnerRegistry.SetPaused(r.Context(), runnerID, paused); err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("runner %q not found", runnerID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	if h.hub != nil {
		h.hub.PublishRunnerCommand(runnerID, action, runnerPauseScopePayload)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"runnerId": runnerID,
		"action":   action,
		"paused":   paused,
		"success":  true,
	})
}

// HandleShutdownRunner handles PUT /runners/{runnerId}/shutdown — request runner shutdown via SSE command.
func (h *Handler) HandleShutdownRunner(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")

	if _, err := h.runnerRegistry.GetRunner(r.Context(), runnerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("runner %q not found", runnerID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	slog.Info("runner shutdown requested", "runner_id", runnerID, "reason", req.Reason)

	var payload map[string]interface{}
	if req.Reason != "" {
		payload = map[string]interface{}{"reason": req.Reason}
	}
	if h.hub != nil {
		h.hub.PublishRunnerCommand(runnerID, "shutdown", payload)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"runnerId": runnerID,
		"action":   "shutdown",
		"success":  true,
	})
}

// HandleRunnerBridge handles GET /runners/{runnerId}/bridge — upgrades to the
// runner's WebSocket bridge connection (runner-scoped auth).
func (h *Handler) HandleRunnerBridge(w http.ResponseWriter, r *http.Request) {
	if h.bridge == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "bridge not available")
		return
	}
	runnerID := chi.URLParam(r, "runnerId")
	h.bridge.ServeBridge(w, r, runnerID)
}

// HandleUpsertInstance handles PUT /runners/{runnerId}/instances/{instanceId}
// — runner-scoped upsert of an OpenCode instance record.
func (h *Handler) HandleUpsertInstance(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	instanceID := chi.URLParam(r, "instanceId")

	var inst types.OpencodeInstance
	if err := json.NewDecoder(r.Body).Decode(&inst); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	inst.InstanceID = instanceID
	inst.RunnerID = runnerID

	if err := h.runnerRegistry.UpsertInstance(r.Context(), runnerID, inst); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"instance": inst,
	})
}

// HandleDeleteInstance handles DELETE /runners/{runnerId}/instances/{instanceId}
// — runner-scoped removal of an OpenCode instance record.
func (h *Handler) HandleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	instanceID := chi.URLParam(r, "instanceId")

	if err := h.runnerRegistry.DeleteInstance(r.Context(), runnerID, instanceID); err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "instance not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleListRunnerInstances handles GET /runners/{runnerId}/instances — list
// OpenCode instances reported by one runner.
func (h *Handler) HandleListRunnerInstances(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")

	resp, err := h.runnerRegistry.ListInstances(r.Context(), runnerID)
	if err != nil {
		// ListInstances now confirms the runner is registered, so ErrNotFound is
		// reachable here for the first time. Without this branch it fell through
		// to a 500 — a client mistake reported as a server fault, which tells
		// callers to retry and trips alerting on a typo. Name the runner too:
		// a bare "not found" leaves the caller guessing which of the ids in
		// their request was wrong.
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Runner not found: %s", runnerID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	h.decorateInstances(resp.Instances)
	WriteJSON(w, http.StatusOK, resp)
}

// HandleListAllInstances handles GET /instances — list OpenCode instances
// across all runners (PWA overview).
func (h *Handler) HandleListAllInstances(w http.ResponseWriter, r *http.Request) {
	resp, err := h.runnerRegistry.ListAllInstances(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	h.decorateInstances(resp.Instances)
	WriteJSON(w, http.StatusOK, resp)
}

// HandleToggleRunnerFeature handles POST /runners/{runnerId}/features/{featureId}/toggle
// — enables or disables a feature on a runner.
// Feature toggle is pushed to the runner via SSE command channel.
func (h *Handler) HandleToggleRunnerFeature(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	featureID := chi.URLParam(r, "featureId")

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	// Verify runner exists
	_, err := h.runnerRegistry.GetRunner(r.Context(), runnerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "runner not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	action := "disabled"
	if req.Enabled {
		action = "enabled"
	}

	slog.Info("runner feature toggled",
		"runner_id", runnerID,
		"feature_id", featureID,
		"enabled", req.Enabled,
	)

	// Push feature toggle to runner via SSE command channel
	if h.hub != nil {
		h.hub.PublishRunnerCommand(runnerID, "feature_toggle", map[string]interface{}{
			"featureId": featureID,
			"enabled":   req.Enabled,
		})
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"runner_id":  runnerID,
		"feature_id": featureID,
		"action":     action,
	})
}
