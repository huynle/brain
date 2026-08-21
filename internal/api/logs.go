package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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

	// Filter out lines with empty content (protects against wrong field names)
	validLines := req.Lines[:0]
	for _, line := range req.Lines {
		if strings.TrimSpace(line.Content) != "" {
			validLines = append(validLines, line)
		}
	}
	if len(validLines) == 0 {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "lines", Message: "all lines have empty content"},
		})
		return
	}

	// Store in bounded buffer
	accepted := h.logBuffer.Append(projectId, taskId, validLines)

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
			Lines:    validLines,
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

	// Parse pagination params.
	//
	// A request with a limit and NO offset means "the newest limit lines" — the
	// tail. The buffer is an oldest-evicting ring, so head-indexed paging from 0
	// would hand back a stale prefix and leave a hole between the fetched history
	// and the live SSE tail. An explicit offset opts into head-indexed pagination
	// over the retained window (offset 0 == oldest retained line).
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	explicitOffset := -1
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			explicitOffset = parsed
		}
	}

	var (
		lines  []types.LogLine
		total  int
		offset int
	)
	if explicitOffset >= 0 {
		offset = explicitOffset
		lines, total = h.logBuffer.Query(projectId, taskId, offset, limit)
	} else {
		// offset reported back is where the tail window actually starts.
		lines, total, offset = h.logBuffer.Tail(projectId, taskId, limit)
	}
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
