package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/supervision"
)

// HandleControlSessionTail is the bounded projection of the same history used
// by the dashboard. It inherits the control route's authorization.
func (h *Handler) HandleControlSessionTail(w http.ResponseWriter, r *http.Request) {
	limit, maxBytes := 20, 16384
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "invalid limit")
			return
		}
	}
	if value := r.URL.Query().Get("max_bytes"); value != "" {
		maxBytes, err = strconv.Atoi(value)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "invalid max_bytes")
			return
		}
	}
	if limit < 1 || limit > 100 || maxBytes < 1024 || maxBytes > 65536 {
		WriteError(w, http.StatusBadRequest, "Bad Request", "limit must be 1..100 and max_bytes 1024..65536")
		return
	}
	runner, session := chi.URLParam(r, "runnerId"), chi.URLParam(r, "sessionId")
	raw, err := h.bridge.FetchHistory(r.Context(), runner, session)
	if err != nil {
		writeBridgeError(w, err)
		return
	}
	if len(raw) == 0 {
		raw = []byte("[]")
	}
	page, err := supervision.SessionTail(raw, supervisorScope(r, runner+"/"+session), r.URL.Query().Get("after"), limit, maxBytes)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) HandleControlSessionDescendants(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 {
			WriteError(w, 400, "Bad Request", "limit must be 1..100")
			return
		}
		limit = n
	}
	runner, session := chi.URLParam(r, "runnerId"), chi.URLParam(r, "sessionId")
	raw, err := h.bridge.FetchChildren(r.Context(), runner, session, true, 5)
	if err != nil {
		writeBridgeError(w, err)
		return
	}
	if len(raw) == 0 {
		raw = []byte("[]")
	}
	page, err := supervision.ChildrenPage(raw, supervisorScope(r, runner+"/"+session), r.URL.Query().Get("after"), limit)
	if err != nil {
		WriteError(w, 400, "Bad Request", err.Error())
		return
	}
	WriteJSON(w, 200, page)
}
