package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	// controlExecPerMinute caps runner-shell commands per token per minute.
	// This is an interactive terminal — a person types many short commands
	// in a burst — so the cap only exists to stop a runaway loop, not to
	// pace a human.
	controlExecPerMinute = 240
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
	controlExecLimiter   = newActionLimiter(controlExecPerMinute)
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
		Source:    types.EventSourceAPI,
		Timestamp: types.TimeNowUTC(),
		RunnerID:  runnerID,
		Metadata:  md,
	}
	if err := h.events.Ingest(r.Context(), []types.Event{evt}); err != nil {
		// Audit is best-effort, but a rejected event means a misconfigured
		// event type — log loudly so it surfaces rather than silently dropping.
		slog.Warn("control audit event rejected", "type", eventType, "error", err)
	}
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
		// The response is already committed; a write failure here means the
		// client hung up, and there is no status code left to change.
		_, _ = w.Write(respBody)
	} else {
		// The response is already committed; a write failure here means the
		// client hung up, and there is no status code left to change.
		_, _ = w.Write([]byte("{}"))
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

// HandleControlSessionHistory handles
// GET /control/runners/{runnerId}/sessions/{sessionId}/history — the transcript
// of a session by ID, even with no live instance. The runner serves it from a
// running instance if one hosts the session, otherwise reads it from OpenCode's
// on-disk storage. This is how completed/historical sessions are reviewed.
func (h *Handler) HandleControlSessionHistory(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	sessionID := chi.URLParam(r, "sessionId")

	body, err := h.bridge.FetchHistory(r.Context(), runnerID, sessionID)
	if err != nil {
		writeBridgeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if len(body) > 0 {
		// The response is already committed; a write failure here means the
		// client hung up, and there is no status code left to change.
		_, _ = w.Write(body)
	} else {
		// The response is already committed; a write failure here means the
		// client hung up, and there is no status code left to change.
		_, _ = w.Write([]byte("[]"))
	}
}

// controlFilePart is a browser-attached file (e.g. a pasted image). The URL
// is a data: URL (base64) the model receives directly, or a file:// path on
// the runner.
type controlFilePart struct {
	Mime     string `json:"mime"`
	URL      string `json:"url"`
	Filename string `json:"filename,omitempty"`
}

// controlPromptRequest is the browser-facing prompt body.
type controlPromptRequest struct {
	Text  string `json:"text"`
	Agent string `json:"agent,omitempty"`
	Model *struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model,omitempty"`
	Files []controlFilePart `json:"files,omitempty"`
}

// controlPromptMaxBytes bounds a prompt body. Larger than the default proxy
// cap because pasted images arrive as base64 data URLs.
const controlPromptMaxBytes = 24 << 20 // 24 MB

// HandleControlPrompt handles POST .../sessions/{sessionId}/prompt — sends a
// prompt via prompt_async (204; output streams via the events endpoint).
func (h *Handler) HandleControlPrompt(w http.ResponseWriter, r *http.Request) {
	if !controlPromptLimiter.allow(limiterKey(r)) {
		WriteError(w, http.StatusTooManyRequests, "Too Many Requests", "prompt rate limit exceeded")
		return
	}

	var req controlPromptRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, controlPromptMaxBytes)).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	// A prompt needs text or at least one attachment.
	if strings.TrimSpace(req.Text) == "" && len(req.Files) == 0 {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "text", Message: "text or an attachment is required"},
		})
		return
	}

	// Translate to OpenCode's prompt_async shape: a text part (if any)
	// followed by file parts (images paste through as data: URLs).
	parts := make([]map[string]interface{}, 0, 1+len(req.Files))
	if strings.TrimSpace(req.Text) != "" {
		parts = append(parts, map[string]interface{}{"type": "text", "text": req.Text})
	}
	for _, f := range req.Files {
		if f.URL == "" || f.Mime == "" {
			continue
		}
		part := map[string]interface{}{"type": "file", "mime": f.Mime, "url": f.URL}
		if f.Filename != "" {
			part["filename"] = f.Filename
		}
		parts = append(parts, part)
	}
	upstream := map[string]interface{}{"parts": parts}
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
	h.emitControlAudit(r, types.EventControlPromptSent,
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
// with body {response: "once"|"always"|"reject"} — OpenCode's own
// permission vocabulary; the body is proxied to the instance untouched.
func (h *Handler) HandleControlPermission(w http.ResponseWriter, r *http.Request) {
	body, ok := readControlBody(w, r, false)
	if !ok {
		return
	}

	sessionID := chi.URLParam(r, "sessionId")
	permissionID := chi.URLParam(r, "permissionId")
	h.emitControlAudit(r, types.EventControlPermissionResponded,
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

	h.emitControlAudit(r, types.EventControlInstanceSpawned, runnerID, inst.InstanceID,
		map[string]string{"workdir": spec.Workdir})

	WriteJSON(w, http.StatusCreated, map[string]any{
		"success":  true,
		"instance": inst,
	})
}

// HandleControlAbortTask handles POST /control/runners/{runnerId}/tasks/{taskId}/abort.
func (h *Handler) HandleControlAbortTask(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "task id is required")
		return
	}
	if err := h.bridge.AbortTask(r.Context(), runnerID, taskID); err != nil {
		writeBridgeError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
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

	h.emitControlAudit(r, types.EventControlInstanceKilled, runnerID, instanceID, nil)
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

// ─── Runner shell ────────────────────────────────────────────────

// controlExecRequest is the browser-facing runner-shell body.
type controlExecRequest struct {
	Command   string `json:"command"`
	Workdir   string `json:"workdir,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// controlExecSignalRequest carries a signal for a running command.
type controlExecSignalRequest struct {
	Signal string `json:"signal"`
}

// Signal vocabulary accepted by the runner's exec handler. Kept as literals
// rather than importing internal/bridge, which would invert the dependency
// the BridgeService interface exists to avoid.
const (
	execSignalInt  = "int"
	execSignalTerm = "term"
	execSignalKill = "kill"
)

// controlExecAuditCommandMax bounds the command text copied into the audit
// event so a pasted heredoc cannot bloat the event store.
const controlExecAuditCommandMax = 512

// SSE event names on an exec stream.
const (
	execExitEvent      = "exec_exit"
	execTruncatedEvent = "exec_truncated"
)

const (
	// execStreamBuffer is the queue depth of one exec's realtime
	// subscription: burst absorption for output produced faster than the
	// browser reads it.
	//
	// Sized as a MEMORY budget, not a message count. Output arrives in
	// bridge.ExecChunkBytes (16 KB) slices, so the depth is really
	// execStreamMaxBacklogBytes of retained output, and a stalled stream
	// pins that much heap for as long as it stays stalled. Nothing caps
	// concurrent exec streams (the rate limiter bounds starts per minute,
	// not live streams), so this number is multiplied by every backgrounded
	// browser tab sitting on a chatty command — which is why it is 8 MB and
	// not the 64 MB that "never lose a byte under any load" would want.
	// 8 MB still covers realistic bursts whole; past it, output is dropped
	// LOUDLY via exec_truncated rather than silently.
	execStreamMaxBacklogBytes = 8 << 20 // 8 MB
	execStreamChunkBytes      = 16 << 10
	execStreamBuffer          = execStreamMaxBacklogBytes / execStreamChunkBytes // 512

	// execWatchdogInterval is how often a live exec stream re-derives whether
	// its command is still running. It bounds how long the terminal can look
	// busy after the command actually ended.
	execWatchdogInterval = 250 * time.Millisecond
)

// generateExecID mints a server-side ID for one shell command. Client-chosen
// IDs are not accepted: the ID keys the realtime topic that carries the
// command's output, so a caller must not be able to guess or reuse another's.
func generateExecID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice; if it ever does, fall back
		// to a nanosecond stamp rather than serving a predictable ID.
		return fmt.Sprintf("exec_%x", time.Now().UnixNano())
	}
	return "exec_" + hex.EncodeToString(b)
}

// HandleControlExec handles POST /control/runners/{runnerId}/exec — runs one
// shell command on the runner host and streams its output back as SSE.
//
// The shell is deliberately unrestricted: authorization is the control:*
// scope on this route and nothing more (control:* already means arbitrary
// code execution on the runner via spawn/prompt). The command timeout and
// the audit event are correctness and observability, not gating.
//
// Errors raised before the first SSE byte are ordinary JSON errors; once the
// stream is open every outcome — including the command's own failure — is
// carried by the exec_exit event.
func (h *Handler) HandleControlExec(w http.ResponseWriter, r *http.Request) {
	if !controlExecLimiter.allow(limiterKey(r)) {
		WriteError(w, http.StatusTooManyRequests, "Too Many Requests", "exec rate limit exceeded")
		return
	}

	runnerID := chi.URLParam(r, "runnerId")

	var req controlExecRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "command", Message: "command is required"},
		})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "streaming not supported")
		return
	}

	execID := generateExecID()

	// Subscribe to the output topic BEFORE asking the runner to start: a
	// short command can emit output and exit before StartExec's ack returns,
	// and anything published without a subscriber is dropped on the floor.
	//
	// An exec gets its own deep queue rather than the shared default: the
	// fan-out never blocks, so the buffer is the only thing standing between
	// a burst of output and a silently truncated transcript.
	ch, unsub := h.hub.SubscribeWithCapacity(realtime.ExecTopic(runnerID, execID), execStreamBuffer)
	defer unsub()

	if err := h.bridge.StartExec(r.Context(), runnerID, execID,
		req.Command, req.Workdir, req.TimeoutMs); err != nil {
		writeBridgeError(w, err)
		return
	}
	// The bridge tracks this exec from StartExec onwards; releasing it here
	// keeps that state bounded by the number of live streams.
	defer h.bridge.ReleaseExec(runnerID, execID)

	command := req.Command
	if len(command) > controlExecAuditCommandMax {
		command = command[:controlExecAuditCommandMax] + "…"
	}
	h.emitControlAudit(r, types.EventControlExecStarted, runnerID, "", map[string]string{
		"exec_id": execID,
		"workdir": req.Workdir,
		"command": command,
	})

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeSSEEvent(w, "started", map[string]any{
		"exec_id":   execID,
		"runner_id": runnerID,
		"workdir":   req.Workdir,
	})
	flusher.Flush()

	heartbeat := time.NewTicker(DefaultHeartbeatInterval)
	defer heartbeat.Stop()
	// The terminal exec_exit rides the same droppable fan-out as the output,
	// and a runner that is killed mid-command never sends one at all. Both
	// would leave this stream — and the terminal in the browser — running
	// forever, so its end is also derived from state the bridge records.
	watchdog := time.NewTicker(execWatchdogInterval)
	defer watchdog.Stop()

	// Count of dropped chunks already reported to the client, so each loss
	// is marked once.
	reportedDrops := 0

	for {
		select {
		case <-r.Context().Done():
			// The browser is gone. Nothing else will ever read this
			// command's output, so don't leave the process running on the
			// runner. r.Context() is already cancelled, hence a fresh one.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := h.bridge.SignalExec(ctx, runnerID, execID, execSignalTerm); err != nil {
				slog.Warn("control exec: abandoned command not signalled",
					"runner_id", runnerID, "exec_id", execID, "error", err)
			}
			cancel()
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Exactly one exec_exit is emitted per command; it ends the
			// stream (and skips the disconnect-signal path above).
			if h.writeExecMessage(w, flusher, runnerID, execID, msg, &reportedDrops) {
				return
			}
		case <-watchdog.C:
			// Drain what is already queued first: exec_exit must never
			// overtake output still sitting in the buffer ahead of it.
			terminated, closed := h.drainExecBacklog(w, flusher, runnerID, execID, ch, &reportedDrops)
			if terminated || closed {
				return
			}
			outcome, tracked := h.bridge.ExecOutcome(runnerID, execID)
			if tracked {
				// A long-running command reports its losses as they happen
				// rather than only at exit.
				h.writeExecTruncation(w, flusher, execID, outcome, &reportedDrops)
			}
			switch {
			case tracked && outcome.Done:
				// The command finished but its exec_exit was lost on the way
				// here; the bridge's record still ends the stream.
			case h.bridge.Connected(runnerID):
				continue // still running
			default:
				outcome = types.ExecOutcome{
					ExitCode: types.ExecExitUnknown,
					Error:    "runner disconnected",
				}
			}
			// Drain once more before terminating. The bridge publishes a
			// command's last chunk and then records its outcome as two steps,
			// so output can land in the queue in the window between the drain
			// above and the ExecOutcome read — and that tail is the part the
			// user actually reads. It arrived intact, so nothing counted it as
			// dropped: ending the stream without this second drain would
			// discard it silently.
			if terminated, closed := h.drainExecBacklog(w, flusher, runnerID, execID, ch, &reportedDrops); terminated || closed {
				return
			}
			writeExecExit(w, execID, outcome)
			flusher.Flush()
			return
		case <-heartbeat.C:
			writeSSEEvent(w, "heartbeat", map[string]any{
				"type":      "heartbeat",
				"timestamp": types.TimeNowUTC().Format(time.RFC3339),
			})
			flusher.Flush()
		}
	}
}

// drainExecBacklog writes every exec event already queued, without blocking.
// terminated reports that the command's exit event was among them; closed
// reports that the subscription ended.
func (h *Handler) drainExecBacklog(
	w http.ResponseWriter, flusher http.Flusher,
	runnerID, execID string, ch <-chan realtime.SSEMessage, reported *int,
) (terminated, closed bool) {
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return false, true
			}
			if h.writeExecMessage(w, flusher, runnerID, execID, msg, reported) {
				return true, false
			}
		default:
			return false, false
		}
	}
}

// writeExecMessage writes one fanned-out exec event and reports whether it
// was the terminal one.
func (h *Handler) writeExecMessage(
	w http.ResponseWriter, flusher http.Flusher,
	runnerID, execID string, msg realtime.SSEMessage, reported *int,
) bool {
	last := msg.Event == execExitEvent
	if last {
		// Any loss must be marked before the exit, or a client that stops
		// reading at exec_exit never learns its transcript is incomplete.
		if outcome, ok := h.bridge.ExecOutcome(runnerID, execID); ok {
			h.writeExecTruncation(w, flusher, execID, outcome, reported)
		}
	}
	writeSSEEvent(w, msg.Event, msg.Data)
	flusher.Flush()
	return last
}

// writeExecTruncation emits an explicit marker for output the fan-out could
// not deliver. Silent truncation is the worst possible outcome here: a user
// acting on a partial transcript has no way to tell that it is partial.
// reported carries what has already been announced, so each loss is marked
// exactly once.
func (h *Handler) writeExecTruncation(
	w http.ResponseWriter, flusher http.Flusher, execID string,
	outcome types.ExecOutcome, reported *int,
) {
	if outcome.DroppedChunks <= *reported {
		return
	}
	*reported = outcome.DroppedChunks
	writeSSEEvent(w, execTruncatedEvent, map[string]any{
		"exec_id":        execID,
		"dropped_chunks": outcome.DroppedChunks,
		"dropped_bytes":  outcome.DroppedBytes,
	})
	flusher.Flush()
}

// writeExecExit emits the terminal event from the bridge's recorded outcome,
// for the paths where the runner's own exec_exit never reached this handler.
func writeExecExit(w http.ResponseWriter, execID string, outcome types.ExecOutcome) {
	writeSSEEvent(w, execExitEvent, map[string]any{
		"exec_id":   execID,
		"exit_code": outcome.ExitCode,
		"error":     outcome.Error,
	})
}

// HandleControlExecSignal handles
// POST /control/runners/{runnerId}/exec/{execId}/signal — delivers ^C
// ("int"), a polite stop ("term") or a hard kill ("kill") to a running
// command. Signalling is a separate request because the exec request itself
// is occupied streaming output.
func (h *Handler) HandleControlExecSignal(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	execID := chi.URLParam(r, "execId")
	if execID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "exec id is required")
		return
	}

	var req controlExecSignalRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	switch req.Signal {
	case execSignalInt, execSignalTerm, execSignalKill:
	default:
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "signal", Message: `signal must be one of "int", "term", "kill"`},
		})
		return
	}

	if err := h.bridge.SignalExec(r.Context(), runnerID, execID, req.Signal); err != nil {
		writeBridgeError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
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
