package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// HandleGetSections handles GET /entries/{id}/sections.
func (h *Handler) HandleGetSections(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	resp, err := h.brain.GetSections(r.Context(), id)
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

// HandleGetSection handles GET /entries/{id}/sections/{title}.
func (h *Handler) HandleGetSection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	title := chi.URLParam(r, "title")

	includeSubsections := r.URL.Query().Get("includeSubsections") == "true"

	resp, err := h.brain.GetSection(r.Context(), id, title, includeSubsections)
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
