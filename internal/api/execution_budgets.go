package api

import (
	"context"
	"net/http"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

type ExecutionBudgetStore interface {
	ConfigureExecutionBudget(context.Context, types.ExecutionBudget, int) (bool, error)
	ExecutionBudget(context.Context, string, string, time.Time) (*types.ExecutionBudget, int64, string, error)
	ReserveBudget(context.Context, string, string, string, string, int64, time.Time) (*types.BudgetReservation, bool, error)
	SettleBudget(context.Context, string, string, string, string) error
}

func WithExecutionBudgets(store ExecutionBudgetStore) HandlerOption {
	return func(h *Handler) { h.executionBudgets = store }
}
func (h *Handler) HandleExecutionBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		budget, used, window, err := h.executionBudgets.ExecutionBudget(r.Context(), r.URL.Query().Get("project"), r.URL.Query().Get("id"), time.Now())
		if err != nil {
			WriteError(w, 500, "Internal Server Error", err.Error())
			return
		}
		if budget == nil {
			WriteError(w, 404, "Not Found", "budget not found")
			return
		}
		WriteJSON(w, 200, map[string]any{"budget": budget, "consumed_and_reserved": used, "window": window, "remaining": max(int64(0), budget.Limit-used), "enforcement": "atomic reservation admission; executor work outside this protocol is not metered", "token_usage": nil, "monetary_cost": nil})
		return
	}
	var req struct {
		Action           string                `json:"action"`
		Budget           types.ExecutionBudget `json:"budget"`
		ExpectedRevision int                   `json:"expected_revision"`
		ReservationID    string                `json:"reservation_id"`
		ParentID         string                `json:"parent_id"`
		Units            int64                 `json:"units"`
	}
	if !decodeBulkJob(w, r, &req) {
		return
	}
	if req.Budget.Project == "" || req.Budget.ID == "" || len(req.Budget.ID) > 128 || len(req.Budget.Project) > 128 || len(req.ReservationID) > 128 || len(req.ParentID) > 128 || len(req.Budget.Unit) > 64 {
		WriteError(w, 400, "Bad Request", "invalid budget identity")
		return
	}
	switch req.Action {
	case "configure":
		ok, err := h.executionBudgets.ConfigureExecutionBudget(r.Context(), req.Budget, req.ExpectedRevision)
		if err != nil {
			WriteError(w, 400, "Bad Request", err.Error())
			return
		}
		if !ok {
			WriteError(w, 409, "Conflict", "budget revision changed or immutable timezone/unit differs")
			return
		}
		WriteJSON(w, 200, map[string]any{"revision": req.ExpectedRevision + 1})
	case "reserve":
		reservation, created, err := h.executionBudgets.ReserveBudget(r.Context(), req.Budget.Project, req.Budget.ID, req.ReservationID, req.ParentID, req.Units, time.Now())
		if err != nil {
			WriteError(w, 409, "Conflict", err.Error())
			return
		}
		WriteJSON(w, 200, map[string]any{"reservation": reservation, "created": created})
	case "commit", "cancel":
		state := "committed"
		if req.Action == "cancel" {
			state = "cancelled"
		}
		if err := h.executionBudgets.SettleBudget(r.Context(), req.Budget.Project, req.Budget.ID, req.ReservationID, state); err != nil {
			WriteError(w, 409, "Conflict", err.Error())
			return
		}
		WriteJSON(w, 200, map[string]any{"state": state})
	default:
		WriteError(w, 400, "Bad Request", "unknown budget action")
	}
}
