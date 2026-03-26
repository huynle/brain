package api

import (
	"encoding/json"
	"net/http"

	"github.com/huynle/brain-api/internal/types"
)

// HandleSearch handles POST /search.
func (h *Handler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	var req types.SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	if req.Query == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "query", Message: "required"},
		})
		return
	}

	resp, err := h.brain.Search(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleInject handles POST /inject.
func (h *Handler) HandleInject(w http.ResponseWriter, r *http.Request) {
	var req types.InjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	if req.Query == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "query", Message: "required"},
		})
		return
	}

	resp, err := h.brain.Inject(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}
