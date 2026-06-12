package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// Control plane: authenticated REST/SSE surface for remote OpenCode access.
// All routes live under /api/v1/control and require the control:* scope
// (admin:* passes). Requests are tunneled to the owning runner over its
// bridge connection; the API never dials runner machines directly.

const (
	// controlPromptPerMinute caps prompt sends per token per minute.
	controlPromptPerMinute = 30
	// controlSpawnPerMinute caps ad-hoc spawns per token per minute.
	controlSpawnPerMinute = 6
)

// actionLimiter is a fixed-window per-key rate limiter for sensitive
// control actions (prompt, spawn).
type actionLimiter struct {
	mu     sync.Mutex
	window time.Time
	counts map[string]int
	max    int
}

func newActionLimiter(max int) *actionLimiter {
	return &actionLimiter{counts: make(map[string]int), max: max}
}

func (l *actionLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.window) >= time.Minute {
		l.window = now
		l.counts = make(map[string]int)
	}
	l.counts[key]++
	return l.counts[key] <= l.max
}

var (
	controlPromptLimiter = newActionLimiter(controlPromptPerMinute)
	controlSpawnLimiter  = newActionLimiter(controlSpawnPerMinute)
)

// limiterKey identifies the acting token for rate limiting.
func limiterKey(r *http.Request) string {
	if auth, ok := AuthResultFromContext(r.Context()); ok {
		return auth.Type + ":" + auth.Name
	}
	return "anonymous"
}

// emitControlAudit publishes a control.* domain event so remote-control
// actions are visible in /events/recent and webhooks.
func (h *Handler) emitControlAudit(r *http.Request, eventType, runnerID, instanceID string, meta map[string]string) {
	if h.events == nil {
		return
	}
	md := map[string]string{"instance_id": instanceID}
	if auth, ok := AuthResultFromContext(r.Context()); ok {
		md["actor"] = auth.Name
		md["auth_type"] = auth.Type
	}
	for k, v := range meta {
		md[k] = v
	}
	evt := types.Event{
		Type:      eventType,
		Source:    "api",
		Timestamp: types.TimeNowUTC(),
		RunnerID:  runnerID,
		Metadata:  md,
	}
	_ = h.events.Ingest(r.Context(), []types.Event{evt})
}

// controlProxy forwards a request to an instance over the bridge and writes
// the upstream status and JSON body through to the client.
func (h *Handler) controlProxy(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	runnerID := chi.URLParam(r, "runnerId")
	instanceID := chi.URLParam(r, "instanceId")

	status, respBody, err := h.bridge.Do(r.Context(), runnerID, instanceID, method, path, body)
	if err != nil {
		writeBridgeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if len(respBody) > 0 {
		w.Write(respBody)
	} else {
		w.Write([]byte("{}"))
	}
}

// writeBridgeError maps bridge failures to HTTP statuses.
func writeBridgeError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not connected"):
		WriteError(w, http.StatusBadGateway, "Bad Gateway", "runner bridge not connected")
	case strings.Contains(msg, "allowlist"):
		WriteError(w, http.StatusForbidden, "Forbidden", msg)
	case strings.Contains(msg, "timed out"):
		WriteError(w, http.StatusGatewayTimeout, "Gateway Timeout", msg)
	case strings.Contains(msg, "unknown instance"), strings.Contains(msg, "not yet discovered"):
		WriteError(w, http.StatusNotFound, "Not Found", msg)
	default:
		WriteError(w, http.StatusBadGateway, "Bad Gateway", msg)
	}
}

// ─── Sessions ────────────────────────────────────────────────────

// HandleControlListSessions handles
// GET /control/runners/{runnerId}/instances/{instanceId}/sessions.
func (h *Handler) HandleControlListSessions(w http.ResponseWriter, r *http.Request) {
	h.controlProxy(w, r, http.MethodGet, "/session", nil)
}

// HandleControlCreateSession handles
// POST /control/runners/{runnerId}/instances/{instanceId}/sessions.
func (h *Handler) HandleControlCreateSession(w http.ResponseWriter, r *http.Request) {
	body, ok := readControlBody(w, r, true)
	if !ok {
		return
	}
	h.controlProxy(w, r, http.MethodPost, "/session", body)
}

// HandleControlSessionStatus handles
// GET /control/runners/{runnerId}/instances/{instanceId}/sessions/status.
func (h *Handler) HandleControlSessionStatus(w http.ResponseWriter, r *http.Request) {
	h.controlProxy(w, r, http.MethodGet, "/session/status", nil)
}

// HandleControlListMessages handles
// GET .../sessions/{sessionId}/messages?limit=N.
func (h *Handler) HandleControlListMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	path := fmt.Sprintf("/session/%s/message", url.PathEscape(sessionID))
	if limit := r.URL.Query().Get("limit"); limit != "" {
		path += "?limit=" + url.QueryEscape(limit)
	}
	h.controlProxy(w, r, http.MethodGet, path, nil)
}

// controlPromptRequest is the browser-facing prompt body.
type controlPromptRequest struct {
	Text  string `json:"text"`
	Agent string `json:"agent,omitempty"`
	Model *struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model,omitempty"`
}

// HandleControlPrompt handles POST .../sessions/{sessionId}/prompt — sends a
// prompt via prompt_async (204; output streams via the events endpoint).
func (h *Handler) HandleControlPrompt(w http.ResponseWriter, r *http.Request) {
	if !controlPromptLimiter.allow(limiterKey(r)) {
		WriteError(w, http.StatusTooManyRequests, "Too Many Requests", "prompt rate limit exceeded")
		return
	}

	var req controlPromptRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "text", Message: "text is required"},
		})
		return
	}

	// Translate to OpenCode's prompt_async shape.
	upstream := map[string]interface{}{
		"parts": []map[string]interface{}{
			{"type": "text", "text": req.Text},
		},
	}
	if req.Agent != "" {
		upstream["agent"] = req.Agent
	}
	if req.Model != nil && req.Model.ProviderID != "" && req.Model.ModelID != "" {
		upstream["model"] = map[string]string{
			"providerID": req.Model.ProviderID,
			"modelID":    req.Model.ModelID,
		}
	}
	body, err := json.Marshal(upstream)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	sessionID := chi.URLParam(r, "sessionId")
	h.emitControlAudit(r, "control.prompt_sent",
		chi.URLParam(r, "runnerId"), chi.URLParam(r, "instanceId"),
		map[string]string{"session_id": sessionID})

	path := fmt.Sprintf("/session/%s/prompt_async", url.PathEscape(sessionID))
	h.controlProxy(w, r, http.MethodPost, path, body)
}

// HandleControlAbort handles POST .../sessions/{sessionId}/abort.
func (h *Handler) HandleControlAbort(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	path := fmt.Sprintf("/session/%s/abort", url.PathEscape(sessionID))
	h.controlProxy(w, r, http.MethodPost, path, nil)
}

// HandleControlPermission handles
// POST .../sessions/{sessionId}/permissions/{permissionId}
// with body {response: "allow"|"deny", remember?: "once"|"always"}.
func (h *Handler) HandleControlPermission(w http.ResponseWriter, r *http.Request) {
	body, ok := readControlBody(w, r, false)
	if !ok {
		return
	}

	sessionID := chi.URLParam(r, "sessionId")
	permissionID := chi.URLParam(r, "permissionId")
	h.emitControlAudit(r, "control.permission_responded",
		chi.URLParam(r, "runnerId"), chi.URLParam(r, "instanceId"),
		map[string]string{"session_id": sessionID, "permission_id": permissionID})

	path := fmt.Sprintf("/session/%s/permissions/%s",
		url.PathEscape(sessionID), url.PathEscape(permissionID))
	h.controlProxy(w, r, http.MethodPost, path, body)
}

// HandleControlPendingPermissions handles GET .../permissions — pending
// permission prompts from the bridge's always-on control-event cache.
func (h *Handler) HandleControlPendingPermissions(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	instanceID := chi.URLParam(r, "instanceId")
	pending := h.bridge.PendingPermissions(runnerID, instanceID)
	if pending == nil {
		pending = []json.RawMessage{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"permissions": pending,
		"total":       len(pending),
	})
}

// ─── Composer metadata ───────────────────────────────────────────

// HandleControlAgents handles GET .../agents (proxied GET /agent).
func (h *Handler) HandleControlAgents(w http.ResponseWriter, r *http.Request) {
	h.controlProxy(w, r, http.MethodGet, "/agent", nil)
}

// HandleControlProviders handles GET .../providers (proxied GET /config/providers).
func (h *Handler) HandleControlProviders(w http.ResponseWriter, r *http.Request) {
	h.controlProxy(w, r, http.MethodGet, "/config/providers", nil)
}

// ─── Spawn / kill ────────────────────────────────────────────────

// HandleControlSpawn handles POST /control/runners/{runnerId}/instances —
// spawns a fresh ad-hoc OpenCode instance on the runner.
func (h *Handler) HandleControlSpawn(w http.ResponseWriter, r *http.Request) {
	if !controlSpawnLimiter.allow(limiterKey(r)) {
		WriteError(w, http.StatusTooManyRequests, "Too Many Requests", "spawn rate limit exceeded")
		return
	}

	runnerID := chi.URLParam(r, "runnerId")

	var spec types.SpawnInstanceSpec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&spec); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	// Fast-fail validation; the runner enforces allowed roots.
	if spec.Workdir == "" || !filepath.IsAbs(spec.Workdir) {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "workdir", Message: "workdir must be an absolute path"},
		})
		return
	}

	inst, err := h.bridge.SpawnInstance(r.Context(), runnerID, spec)
	if err != nil {
		writeBridgeError(w, err)
		return
	}

	// Upsert into the registry so the instance is visible immediately
	// (the runner's own report/heartbeat reconcile is the source of truth).
	if h.runnerRegistry != nil {
		_ = h.runnerRegistry.UpsertInstance(r.Context(), runnerID, *inst)
	}

	h.emitControlAudit(r, "control.instance_spawned", runnerID, inst.InstanceID,
		map[string]string{"workdir": spec.Workdir})

	WriteJSON(w, http.StatusCreated, map[string]any{
		"success":  true,
		"instance": inst,
	})
}

// HandleControlKill handles DELETE /control/runners/{runnerId}/instances/{instanceId}
// — terminates an ad-hoc instance. Task instances are refused: they are
// owned by the task lifecycle.
func (h *Handler) HandleControlKill(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	instanceID := chi.URLParam(r, "instanceId")

	// Fast-fail on known task instances; the runner re-validates.
	if h.runnerRegistry != nil {
		if inst, err := h.runnerRegistry.GetInstance(r.Context(), runnerID, instanceID); err == nil &&
			inst != nil && inst.Kind == types.InstanceKindTask {
			WriteError(w, http.StatusConflict, "Conflict",
				"task instances are owned by the task lifecycle; cancel the task instead")
			return
		}
	}

	if err := h.bridge.KillInstance(r.Context(), runnerID, instanceID); err != nil {
		writeBridgeError(w, err)
		return
	}

	if h.runnerRegistry != nil {
		err := h.runnerRegistry.DeleteInstance(r.Context(), runnerID, instanceID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
			return
		}
	}

	h.emitControlAudit(r, "control.instance_killed", runnerID, instanceID, nil)
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ─── Event stream ────────────────────────────────────────────────

// HandleControlEvents handles GET .../events — streams instance events to
// the browser via SSE. Attaching opens the full upstream event firehose
// (refcounted across browsers); pending permissions are replayed as
// synthetic first events so a late-attaching client renders consistent state.
func (h *Handler) HandleControlEvents(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	instanceID := chi.URLParam(r, "instanceId")

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "streaming not supported")
		return
	}

	// Subscribe to the fan-out topic BEFORE opening the upstream stream so
	// no events are dropped in between.
	ch, unsub := h.hub.Subscribe(realtime.InstanceTopic(runnerID, instanceID))
	defer unsub()

	release, err := h.bridge.AcquireStream(runnerID, instanceID)
	if err != nil {
		writeBridgeError(w, err)
		return
	}
	defer release()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	now := types.TimeNowUTC().Format(time.RFC3339)
	writeSSEEvent(w, "connected", map[string]any{
		"type":        "connected",
		"transport":   "sse",
		"timestamp":   now,
		"runner_id":   runnerID,
		"instance_id": instanceID,
	})

	// Replay pending permissions so the client shows live approval prompts.
	for _, raw := range h.bridge.PendingPermissions(runnerID, instanceID) {
		writeSSEEvent(w, "instance_event", raw)
	}
	flusher.Flush()

	heartbeat := time.NewTicker(DefaultHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEvent(w, msg.Event, msg.Data)
			flusher.Flush()
		case <-heartbeat.C:
			writeSSEEvent(w, "heartbeat", map[string]any{
				"type":      "heartbeat",
				"timestamp": types.TimeNowUTC().Format(time.RFC3339),
			})
			flusher.Flush()
		}
	}
}

// readControlBody reads a size-capped JSON body for pass-through proxying.
// allowEmpty permits an empty body (proxied as null).
func readControlBody(w http.ResponseWriter, r *http.Request, allowEmpty bool) ([]byte, bool) {
	var raw json.RawMessage
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw)
	if err != nil {
		if allowEmpty {
			return nil, true
		}
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return nil, false
	}
	return raw, true
}
