package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// attentionRecipient resolves the inbox owner for a request. Every attention
// item is per-user, so reads and state changes are scoped to the authenticated
// principal's name. This is what keeps one user from seeing or mutating
// another's items even under a shared admin token — the token NAME is the
// recipient identity.
func attentionRecipient(r *http.Request) string {
	if auth, ok := AuthResultFromContext(r.Context()); ok && auth.Name != "" {
		return auth.Name
	}
	return "local"
}

func (h *Handler) attentionServiceReady(w http.ResponseWriter) bool {
	if h.attention == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "attention service not configured")
		return false
	}
	return true
}

// HandleListAttention handles GET /attention.
func (h *Handler) HandleListAttention(w http.ResponseWriter, r *http.Request) {
	if !h.attentionServiceReady(w) {
		return
	}
	q := r.URL.Query()
	f := types.AttentionListFilter{
		Recipient:      attentionRecipient(r),
		State:          q.Get("state"),
		Project:        q.Get("project"),
		Kind:           q.Get("kind"),
		Severity:       q.Get("severity"),
		SourceType:     q.Get("source_type"),
		IncludeSnoozed: q.Get("include_snoozed") == "true",
	}
	items, err := h.attention.ListAttention(r.Context(), f)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if items == nil {
		items = []types.Attention{}
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{"attention": items, "count": len(items)})
}

// HandleAttentionCounts handles GET /attention/counts.
func (h *Handler) HandleAttentionCounts(w http.ResponseWriter, r *http.Request) {
	if !h.attentionServiceReady(w) {
		return
	}
	counts, err := h.attention.AttentionCounts(r.Context(), attentionRecipient(r))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, counts)
}

// HandleGetAttention handles GET /attention/{id}.
func (h *Handler) HandleGetAttention(w http.ResponseWriter, r *http.Request) {
	if !h.attentionServiceReady(w) {
		return
	}
	item, err := h.attention.GetAttention(r.Context(), attentionRecipient(r), chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if item == nil {
		WriteError(w, http.StatusNotFound, "Not Found", "attention item not found")
		return
	}
	WriteJSON(w, http.StatusOK, item)
}

// HandleCreateAttention handles POST /attention.
func (h *Handler) HandleCreateAttention(w http.ResponseWriter, r *http.Request) {
	if !h.attentionServiceReady(w) {
		return
	}
	var req types.CreateAttentionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON: "+err.Error())
		return
	}
	item, err := h.attention.CreateAttention(r.Context(), attentionRecipient(r), req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	WriteJSON(w, http.StatusCreated, item)
}

// attentionTransition is the shared body of the state-change action routes.
func (h *Handler) attentionTransition(w http.ResponseWriter, r *http.Request, state string) {
	if !h.attentionServiceReady(w) {
		return
	}
	var body struct {
		SnoozedUntil string `json:"snoozed_until"`
	}
	// A body is optional for every transition except snooze; ignore decode
	// errors on an empty body.
	_ = json.NewDecoder(r.Body).Decode(&body)
	item, err := h.attention.SetAttentionState(r.Context(), attentionRecipient(r),
		chi.URLParam(r, "id"), state, body.SnoozedUntil)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, item)
}

// HandleReadAttention handles POST /attention/{id}/read.
func (h *Handler) HandleReadAttention(w http.ResponseWriter, r *http.Request) {
	h.attentionTransition(w, r, types.AttentionStateRead)
}

// HandleUnreadAttention handles POST /attention/{id}/unread.
func (h *Handler) HandleUnreadAttention(w http.ResponseWriter, r *http.Request) {
	h.attentionTransition(w, r, types.AttentionStateUnread)
}

// HandleSnoozeAttention handles POST /attention/{id}/snooze.
func (h *Handler) HandleSnoozeAttention(w http.ResponseWriter, r *http.Request) {
	h.attentionTransition(w, r, types.AttentionStateSnoozed)
}

// HandleResolveAttention handles POST /attention/{id}/resolve.
func (h *Handler) HandleResolveAttention(w http.ResponseWriter, r *http.Request) {
	h.attentionTransition(w, r, types.AttentionStateResolved)
}

// HandleDismissAttention handles POST /attention/{id}/dismiss.
func (h *Handler) HandleDismissAttention(w http.ResponseWriter, r *http.Request) {
	h.attentionTransition(w, r, types.AttentionStateDismissed)
}
