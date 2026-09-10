package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

type SupervisorOperationStore interface {
	BeginSupervisorOperation(context.Context, string, string, string, string) (bool, error)
	SupervisorOperation(context.Context, string, string) (*types.SupervisorOperation, string, error)
	FinishSupervisorOperation(context.Context, string, string, string, string) error
}

func WithSupervisorOperations(store SupervisorOperationStore) HandlerOption {
	return func(h *Handler) { h.supervisorOperations = store }
}

type supervisorCommand struct {
	ID                 string `json:"id"`
	Operation          string `json:"operation"`
	Project            string `json:"project"`
	TaskID             string `json:"task_id"`
	RunnerID           string `json:"runner_id"`
	InstanceID         string `json:"instance_id"`
	SessionID          string `json:"session_id"`
	Text               string `json:"text"`
	BudgetID           string `json:"budget_id,omitempty"`
	BudgetUnits        int64  `json:"budget_units,omitempty"`
	ParentReservation  string `json:"parent_reservation,omitempty"`
	CheckpointID       string `json:"checkpoint_id,omitempty"`
	CheckpointRevision int    `json:"checkpoint_revision,omitempty"`
}

func (h *Handler) HandleSupervisorOperation(w http.ResponseWriter, r *http.Request) {
	actor := supervisorScope(r, "operations")
	if r.Method == http.MethodGet {
		receipt, _, err := h.supervisorOperations.SupervisorOperation(r.Context(), actor, chi.URLParam(r, "operationId"))
		if err != nil {
			WriteError(w, 500, "Internal Server Error", err.Error())
			return
		}
		if receipt == nil {
			WriteError(w, 404, "Not Found", "operation not found")
			return
		}
		WriteJSON(w, 200, receipt)
		return
	}
	var command supervisorCommand
	if !decodeBulkJob(w, r, &command) {
		return
	}
	if len(command.ID) < 8 || len(command.ID) > 128 || len(command.Text) > 65536 {
		WriteError(w, 400, "Bad Request", "id must be 8..128 bytes; text at most 65536 bytes")
		return
	}
	switch command.Operation {
	case "prompt":
		if command.RunnerID == "" || command.InstanceID == "" || command.SessionID == "" || command.Text == "" || h.bridge == nil {
			WriteError(w, 400, "Bad Request", "prompt needs runner, instance, session and text")
			return
		}
	case "resume_with_context":
		if command.Project == "" || command.TaskID == "" || command.Text == "" || h.tasks == nil {
			WriteError(w, 400, "Bad Request", "resume needs project, task and text")
			return
		}
	case "trigger":
		if command.Project == "" || command.TaskID == "" || h.tasks == nil {
			WriteError(w, 400, "Bad Request", "trigger needs project and task")
			return
		}
	default:
		WriteError(w, 400, "Bad Request", "unsupported operation")
		return
	}
	raw, _ := json.Marshal(command)
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	created, err := h.supervisorOperations.BeginSupervisorOperation(r.Context(), actor, command.ID, command.Operation, digest)
	if err != nil {
		WriteError(w, 500, "Internal Server Error", err.Error())
		return
	}
	if !created {
		receipt, prior, err := h.supervisorOperations.SupervisorOperation(r.Context(), actor, command.ID)
		if err != nil {
			WriteError(w, 500, "Internal Server Error", err.Error())
			return
		}
		if prior != digest {
			WriteError(w, 409, "Conflict", "id already belongs to a different payload")
			return
		}
		WriteJSON(w, 200, receipt)
		return
	}
	// The durable receipt owns this attempt after admission, even if the HTTP
	// caller disconnects. No request payload or credentials are persisted.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 25*time.Second)
	defer cancel()
	// Capture an explicit, artifact-bound handoff before delivery. Never silently
	// inject a newer answer than the caller reviewed.
	preparationError := ""
	if command.CheckpointID != "" {
		if h.supervisorCheckpoints == nil {
			preparationError = "checkpoints unavailable"
		} else {
			checkpoint, loadErr := h.supervisorCheckpoints.SupervisorCheckpoint(ctx, command.Project, command.CheckpointID)
			if loadErr != nil || checkpoint == nil || checkpoint.Revision != command.CheckpointRevision || (checkpoint.TaskID != "" && checkpoint.TaskID != command.TaskID) {
				preparationError = "checkpoint missing, stale or outside task scope"
			} else {
				handoff, _ := json.Marshal(checkpoint)
				command.Text += "\n\nSupervisor handoff (recorded evidence, not additional authorization):\n" + string(handoff)
			}
		}
	}
	if command.BudgetID != "" && preparationError == "" {
		if h.executionBudgets == nil || command.Project == "" {
			preparationError = "budget unavailable or project missing"
		} else {
			reservation, _, reserveErr := h.executionBudgets.ReserveBudget(ctx, command.Project, command.BudgetID, command.ID, command.ParentReservation, command.BudgetUnits, time.Now())
			if reserveErr != nil || reservation == nil || reservation.State == "cancelled" {
				preparationError = "budget admission refused"
			} else if settleErr := h.executionBudgets.SettleBudget(ctx, command.Project, command.BudgetID, command.ID, "committed"); settleErr != nil {
				preparationError = "budget charge could not be confirmed"
			}
		}
	}
	if preparationError != "" {
		_ = h.supervisorOperations.FinishSupervisorOperation(ctx, actor, command.ID, "rejected", preparationError)
		WriteError(w, 409, "Conflict", preparationError)
		return
	}
	state, detail := "delivered", "executor accepted guidance; this is not task completion"
	switch command.Operation {
	case "prompt":
		body, _ := json.Marshal(map[string]any{"parts": []map[string]string{{"type": "text", "text": command.Text}}})
		var code int
		code, _, err = h.bridge.Do(ctx, command.RunnerID, command.InstanceID, http.MethodPost, "/session/"+url.PathEscape(command.SessionID)+"/prompt_async", body)
		if err == nil && (code < 200 || code >= 300) {
			state = "outcome_unknown"
			detail = "executor rejected the request; inspect session state before retrying"
		}
	case "resume_with_context":
		var result *types.ResumeWithContextResult
		result, err = h.tasks.ResumeTaskWithContext(ctx, command.Project, command.TaskID, &types.ResumeWithContextOptions{InjectedContext: command.Text, PreferSameSession: true})
		if err == nil && result != nil && !result.InjectedLive {
			state = "accepted"
			detail = "resume recorded for runner dispatch; not proof of executor delivery or task completion"
		}
	case "trigger":
		_, err = h.tasks.TriggerTask(ctx, command.Project, command.TaskID)
		state = "accepted"
		detail = "trigger recorded; not proof of execution or task completion"
	}
	if err != nil {
		state = "outcome_unknown"
		detail = "operation returned an error or timed out; reconcile task/session state before issuing a new ID"
	}
	persist, done := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer done()
	if err := h.supervisorOperations.FinishSupervisorOperation(persist, actor, command.ID, state, detail); err != nil {
		WriteError(w, 500, "Internal Server Error", "delivery outcome could not be persisted; query operation ID before retrying")
		return
	}
	receipt, _, err := h.supervisorOperations.SupervisorOperation(persist, actor, command.ID)
	if err != nil {
		WriteError(w, 500, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, 200, receipt)
}
