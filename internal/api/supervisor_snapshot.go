package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/supervision"
)

func (h *Handler) HandleSupervisorCapabilities(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, 200, map[string]any{"version": config.Version, "commit": config.Commit, "transports": []string{"stdio", "streamable_http"}, "tools": supervision.ToolCapabilities(), "client_installation": "unknown; server registration does not prove client installation, connection or grants"})
}
func (h *Handler) HandleSupervisorSnapshot(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project_id")
	if project == "" {
		WriteError(w, 400, "Bad Request", "project_id required")
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			WriteError(w, 400, "Bad Request", "limit must be 1..100")
			return
		}
		limit = n
	}
	filters := map[string]string{"project_id": project}
	b, _ := json.Marshal(filters)
	// Cursor BEFORE state reads lets events_wait replay concurrent changes.
	cursor := ""
	events, err := h.events.Recent(r.Context(), 1, nil)
	if err != nil {
		WriteError(w, 500, "Internal Server Error", err.Error())
		return
	}
	if len(events) > 0 {
		cursor = events[len(events)-1].ID
	}
	started := time.Now().UTC()
	tasks, err := h.tasks.GetTasks(r.Context(), project)
	if err != nil {
		WriteError(w, 500, "Internal Server Error", err.Error())
		return
	}
	sort.Slice(tasks.Tasks, func(i, j int) bool { return tasks.Tasks[i].ID < tasks.Tasks[j].ID })
	out := []map[string]any{}
	truncated := false
	for _, task := range tasks.Tasks {
		if id := r.URL.Query().Get("task_id"); id != "" && task.ID != id {
			continue
		}
		if feature := r.URL.Query().Get("feature_id"); feature != "" && task.FeatureID != feature {
			continue
		}
		if task.ID <= r.URL.Query().Get("after_task") {
			continue
		}
		if len(out) == limit {
			truncated = true
			break
		}
		out = append(out, map[string]any{"id": task.ID, "feature_id": task.FeatureID, "status": task.Status, "classification": task.Classification, "waiting_on": task.WaitingOn, "delivery_unmet": task.DeliveryVerification.Unmet(), "is_abandoned": task.IsAbandoned})
	}
	next := ""
	if len(out) > 0 {
		next = out[len(out)-1]["id"].(string)
	}
	checkpoints := []map[string]any{}
	checkpointCoverage := "unavailable"
	if h.supervisorCheckpoints != nil {
		rows, readErr := h.supervisorCheckpoints.SupervisorCheckpoints(r.Context(), project, "")
		if readErr == nil {
			checkpointCoverage = "first 100 checkpoints; use supervisor_checkpoint for pagination and evidence"
			for i, row := range rows {
				if i == 100 {
					break
				}
				checkpoints = append(checkpoints, map[string]any{"id": row.ID, "task_id": row.TaskID, "feature_id": row.FeatureID, "artifact": row.Artifact, "state": row.State, "revision": row.Revision})
			}
		}
	}
	WriteJSON(w, 200, map[string]any{"snapshot_started_at": started, "snapshot_finished_at": time.Now().UTC(), "atomic": false, "checkpoints": checkpoints, "checkpoint_coverage": checkpointCoverage, "tasks": out, "truncated": truncated, "after_task": next, "event_cursor": encodeEventCursor(supervisorScope(r, string(b)), cursor), "event_filters": filters, "unavailable_sources": []string{"resource samples are separately timestamped via resource_health"}})
}

type dispatchPreviewService interface {
	DispatchPreview(context.Context, string, string, bool) (map[string]any, error)
}

func (h *Handler) HandleDispatchPreview(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.runTask.(dispatchPreviewService)
	if !ok {
		WriteError(w, 501, "Not Implemented", "dispatch preview unavailable")
		return
	}
	project, task := r.URL.Query().Get("project_id"), r.URL.Query().Get("task_id")
	if project == "" || task == "" {
		WriteError(w, 400, "Bad Request", "project_id and task_id required")
		return
	}
	result, err := svc.DispatchPreview(r.Context(), project, task, r.URL.Query().Get("manual") == "true")
	if err != nil {
		WriteError(w, 400, "Bad Request", err.Error())
		return
	}
	WriteJSON(w, 200, result)
}
