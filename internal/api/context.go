package api

import (
	"encoding/json"
	"net/http"

	"github.com/huynle/brain-api/internal/types"
)

func (h *Handler) HandleResolveClientContext(w http.ResponseWriter, r *http.Request) {
	if h.clientContext == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "client context service not configured")
		return
	}

	var req types.ResolveClientContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	if req.Client.ClientID == "" {
		WriteValidationError(w, []types.ValidationDetail{{Field: "client.client_id", Message: "required"}})
		return
	}
	if req.Client.HostID == "" {
		WriteValidationError(w, []types.ValidationDetail{{Field: "client.host_id", Message: "required"}})
		return
	}

	resp, err := h.clientContext.Resolve(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}
