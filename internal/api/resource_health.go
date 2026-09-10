package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func (h *Handler) HandleResourceHealth(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project_id")
	if project == "" {
		WriteError(w, 400, "Bad Request", "project_id is required")
		return
	}
	filters := map[string]string{"type": types.EventTaskResourceSample, "project_id": project}
	if task := r.URL.Query().Get("task_id"); task != "" {
		filters["task_id"] = task
	}
	events, err := h.events.Recent(r.Context(), 1000, filters)
	if err != nil {
		WriteError(w, 500, "Internal Server Error", err.Error())
		return
	}
	type observation struct {
		RunnerID  string               `json:"runner_id"`
		TaskID    string               `json:"task_id"`
		FeatureID string               `json:"feature_id,omitempty"`
		Sample    types.ResourceSample `json:"sample"`
		Fresh     bool                 `json:"fresh"`
	}
	now := time.Now().UTC()
	out := []observation{}
	seen := map[string]bool{}
	truncated := false
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		key := e.RunnerID + "/" + e.TaskID
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(e.Metadata["resource"]) > 4096 {
			truncated = true
			continue
		}
		e = supervisorEvent(e)
		var sample types.ResourceSample
		if json.Unmarshal([]byte(e.Metadata["resource"]), &sample) != nil {
			continue
		}
		if len(out) == 100 {
			truncated = true
			break
		}
		age := now.Sub(sample.SampledAt)
		out = append(out, observation{e.RunnerID, e.TaskID, e.FeatureID, sample, age >= 0 && age <= 30*time.Second})
	}
	WriteJSON(w, 200, map[string]any{"observed_at": now, "samples": out, "truncated": truncated, "coverage": h.events.Coverage(), "availability": "recent runner samples only; empty means unavailable, including older runners or disabled memory guard", "warning_fraction": 0.8, "clear_fraction": 0.7})
}
