package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// HandleGetStats handles GET /stats.
func (h *Handler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	global := r.URL.Query().Get("global") == "true"

	resp, err := h.brain.GetStats(r.Context(), global)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleGetOrphans handles GET /orphans.
func (h *Handler) HandleGetOrphans(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entryType := q.Get("type")
	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	entries, err := h.brain.GetOrphans(r.Context(), entryType, limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, entries)
}

// HandleGetStale handles GET /stale.
func (h *Handler) HandleGetStale(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entryType := q.Get("type")

	days := 30 // default
	if v := q.Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}

	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	entries, err := h.brain.GetStale(r.Context(), days, entryType, limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, entries)
}

// HandleVerifyEntry handles POST /entries/{id}/verify.
func (h *Handler) HandleVerifyEntry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	resp, err := h.brain.Verify(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleGenerateLink handles POST /link.
func (h *Handler) HandleGenerateLink(w http.ResponseWriter, r *http.Request) {
	var req types.LinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	if req.Path == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "path", Message: "required"},
		})
		return
	}

	resp, err := h.brain.GenerateLink(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}
