package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// graphEntryID reads the {id} path param for the graph routes.
//
// These three routes match a SINGLE path segment, so a client addressing an
// entry by its full path (e.g. "projects/x/task/abc.md") must percent-encode
// the slashes - and internal/mcp/brain_tools.go's fetchGraph does exactly that
// with url.PathEscape. chi does NOT unescape URL params, so the handler
// received "projects%2Fx%2Ftask%2Fabc.md" and no entry ever matched it. The
// full-path form advertised by the backlinks/outlinks/related tool schemas
// therefore never worked; it silently returned an empty list, which read as
// "this entry has no links".
//
// Unescaping here makes the documented form work. A bare 8-char short ID
// contains nothing to unescape and is returned unchanged, so the form that has
// always worked keeps working. A malformed escape sequence falls back to the
// raw value rather than erroring, preserving the previous behaviour for junk
// input (which now surfaces as a 404 from the service).
func graphEntryID(r *http.Request) string {
	raw := chi.URLParam(r, "id")
	if unescaped, err := url.PathUnescape(raw); err == nil {
		return unescaped
	}
	return raw
}

// HandleGetBacklinks handles GET /entries/{id}/backlinks.
func (h *Handler) HandleGetBacklinks(w http.ResponseWriter, r *http.Request) {
	id := graphEntryID(r)

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
	id := graphEntryID(r)

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
	id := graphEntryID(r)

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
