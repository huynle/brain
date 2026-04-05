package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/logbuffer"
	"github.com/huynle/brain-api/internal/types"
)

// WithLogBuffer sets the log buffer on the Handler.
func WithLogBuffer(lb *logbuffer.Buffer) HandlerOption {
	return func(h *Handler) {
		h.logBuffer = lb
	}
}

// HandleIngestLogs handles POST /tasks/{projectId}/{taskId}/logs — ingest log batches from runners.
func (h *Handler) HandleIngestLogs(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	var req types.LogIngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	// Validate required fields
	if req.RunnerID == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "runnerId", Message: "runnerId is required"},
		})
		return
	}

	if len(req.Lines) == 0 {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "lines", Message: "lines must not be empty"},
		})
		return
	}

	// Store in bounded buffer
	accepted := h.logBuffer.Append(projectId, taskId, req.Lines)

	// Re-broadcast via SSE to project subscribers
	if h.hub != nil {
		h.hub.PublishRunnerLog(projectId, types.SSERunnerLogData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventRunnerLog,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
				ProjectID: projectId,
			},
			TaskID:   taskId,
			RunnerID: req.RunnerID,
			Lines:    req.Lines,
		})
	}

	slog.Debug("logs ingested",
		"project", projectId,
		"task", taskId,
		"runner", req.RunnerID,
		"lines", accepted,
	)

	WriteJSON(w, http.StatusOK, types.LogIngestResponse{
		Accepted: accepted,
	})
}

// HandleGetLogs handles GET /tasks/{projectId}/{taskId}/logs — retrieve historical logs.
func (h *Handler) HandleGetLogs(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	// Parse pagination params
	offset := 0
	limit := 100
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	lines, total := h.logBuffer.Query(projectId, taskId, offset, limit)
	if lines == nil {
		lines = []types.LogLine{}
	}

	WriteJSON(w, http.StatusOK, types.LogQueryResponse{
		Lines:  lines,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	})
}
