package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// HandleGetBacklinks handles GET /entries/{id}/backlinks.
func (h *Handler) HandleGetBacklinks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	entries, err := h.brain.GetBacklinks(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, entries)
}

// HandleGetOutlinks handles GET /entries/{id}/outlinks.
func (h *Handler) HandleGetOutlinks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	entries, err := h.brain.GetOutlinks(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, entries)
}

// HandleGetRelated handles GET /entries/{id}/related.
func (h *Handler) HandleGetRelated(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	limit := 10 // default
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	entries, err := h.brain.GetRelated(r.Context(), id, limit)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, entries)
}
