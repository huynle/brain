package api

import (
	"context"
	"net/http"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

type SupervisorCheckpointStore interface {
	SupervisorCheckpointVersions(context.Context, string, string) ([]types.SupervisorCheckpoint, error)
	SupervisorCheckpoints(context.Context, string, string) ([]types.SupervisorCheckpoint, error)
	SupervisorCheckpoint(context.Context, string, string) (*types.SupervisorCheckpoint, error)
	CompareSupervisorCheckpoint(context.Context, int, *types.SupervisorCheckpoint) (bool, error)
}

func WithSupervisorCheckpoints(store SupervisorCheckpointStore) HandlerOption {
	return func(h *Handler) { h.supervisorCheckpoints = store }
}
func (h *Handler) HandleSupervisorCheckpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		project := r.URL.Query().Get("project")
		if project == "" {
			WriteError(w, 400, "Bad Request", "project required")
			return
		}
		if id := r.URL.Query().Get("id"); id != "" {
			rows, err := h.supervisorCheckpoints.SupervisorCheckpointVersions(r.Context(), project, id)
			if err != nil {
				WriteError(w, 500, "Internal Server Error", err.Error())
				return
			}
			WriteJSON(w, 200, map[string]any{"versions": rows, "limit": 100})
			return
		}
		rows, err := h.supervisorCheckpoints.SupervisorCheckpoints(r.Context(), project, r.URL.Query().Get("after"))
		if err != nil {
			WriteError(w, 500, "Internal Server Error", err.Error())
			return
		}
		more := len(rows) > 100
		if more {
			rows = rows[:100]
		}
		next := ""
		if len(rows) > 0 {
			next = rows[len(rows)-1].ID
		}
		WriteJSON(w, 200, map[string]any{"checkpoints": rows, "after": next, "truncated": more})
		return
	}
	var req struct {
		Action           string                     `json:"action"`
		ExpectedRevision int                        `json:"expected_revision"`
		Checkpoint       types.SupervisorCheckpoint `json:"checkpoint"`
	}
	if !decodeBulkJob(w, r, &req) {
		return
	}
	value := req.Checkpoint
	if value.Project == "" || len(value.Project) > 128 || len(value.ID) < 8 || len(value.ID) > 128 || value.Artifact == "" || len(value.Artifact) > 512 || len(value.Question) > 4096 || len(value.Answer) > 8192 || len(value.VerificationReference) > 1024 {
		WriteError(w, 400, "Bad Request", "checkpoint identity or content exceeds bounds")
		return
	}
	current, err := h.supervisorCheckpoints.SupervisorCheckpoint(r.Context(), value.Project, value.ID)
	if err != nil {
		WriteError(w, 500, "Internal Server Error", err.Error())
		return
	}
	if current == nil {
		if req.Action != "request" || req.ExpectedRevision != 0 || value.Question == "" {
			WriteError(w, 409, "Conflict", "new checkpoint requires request and revision zero")
			return
		}
		value.State = "pending"
		value.Answer = ""
		value.AnsweredBy = ""
		value.AnsweredAt = nil
		value.VerificationReference = ""
		value.VerifiedBy = ""
	} else {
		if req.Action == "request" && current.Artifact == value.Artifact && current.Question == value.Question && current.TaskID == value.TaskID && current.FeatureID == value.FeatureID {
			WriteJSON(w, 200, current)
			return
		}
		if current.Revision != req.ExpectedRevision {
			WriteError(w, 409, "Conflict", "checkpoint revision changed")
			return
		}
		if req.Action != "supersede" && value.Artifact != current.Artifact {
			WriteError(w, 409, "Conflict", "answer refers to a different artifact")
			return
		}
		supplied := value
		value = *current
		actor := "local-owner"
		if auth, ok := AuthResultFromContext(r.Context()); ok {
			actor = auth.Type + ":" + auth.Name
		}
		switch req.Action {
		case "answer":
			if supplied.Answer == "" {
				WriteError(w, 400, "Bad Request", "answer required")
				return
			}
			value.Answer = supplied.Answer
			value.AnsweredBy = actor
			now := time.Now().UTC()
			value.AnsweredAt = &now
			value.State = "answered"
			value.VerificationReference = ""
			value.VerifiedBy = ""
		case "verify":
			if value.State != "answered" || supplied.VerificationReference == "" {
				WriteError(w, 409, "Conflict", "verification requires an answer and independent evidence reference")
				return
			}
			value.State = "verified"
			value.VerificationReference = supplied.VerificationReference
			value.VerifiedBy = actor
		case "supersede":
			value.Artifact = supplied.Artifact
			if supplied.Question != "" {
				value.Question = supplied.Question
			}
			value.State = "pending"
			value.Answer = ""
			value.AnsweredAt = nil
			value.AnsweredBy = ""
			value.VerificationReference = ""
			value.VerifiedBy = ""
		default:
			WriteError(w, 400, "Bad Request", "unknown checkpoint action")
			return
		}
	}
	value.Revision = req.ExpectedRevision + 1
	ok, err := h.supervisorCheckpoints.CompareSupervisorCheckpoint(r.Context(), req.ExpectedRevision, &value)
	if err != nil {
		WriteError(w, 500, "Internal Server Error", err.Error())
		return
	}
	if !ok {
		WriteError(w, 409, "Conflict", "checkpoint changed concurrently")
		return
	}
	if h.events != nil {
		_ = h.events.Ingest(r.Context(), []types.Event{{Type: types.EventSupervisorCheckpointChanged, Source: types.EventSourceAPI, ProjectID: value.Project, TaskID: value.TaskID, FeatureID: value.FeatureID}})
	}
	WriteJSON(w, 200, value)
}
