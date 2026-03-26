package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// TokenService defines the interface for token management operations.
type TokenService interface {
	// GenerateToken generates a secure random token string.
	GenerateToken() (string, error)

	// CreateToken stores a new token with the given name and value.
	CreateToken(ctx context.Context, name, token string) error

	// ListTokens returns all tokens. Token values are masked to prefix only.
	// Pass true to include revoked tokens.
	ListTokens(ctx context.Context, includeRevoked ...bool) ([]storage.Token, error)

	// GetTokenByName returns a token by name.
	GetTokenByName(ctx context.Context, name string) (*storage.Token, error)

	// RevokeToken soft-revokes a token by setting revoked_at.
	RevokeToken(ctx context.Context, name string) error
}

// WithTokenService sets the TokenService on the Handler.
func WithTokenService(ts TokenService) HandlerOption {
	return func(h *Handler) {
		h.tokens = ts
	}
}

// createTokenRequest is the request body for POST /tokens.
type createTokenRequest struct {
	Name string `json:"name"`
}

// createTokenResponse is the response for POST /tokens.
type createTokenResponse struct {
	Name      string `json:"name"`
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
}

// tokenListItem is a single token in the list response.
type tokenListItem struct {
	Name        string `json:"name"`
	TokenPrefix string `json:"token_prefix"`
	CreatedAt   string `json:"created_at"`
	LastUsed    string `json:"last_used,omitempty"`
	Status      string `json:"status"`
}

// listTokensResponse is the response for GET /tokens.
type listTokensResponse struct {
	Tokens  []tokenListItem `json:"tokens"`
	Active  int             `json:"active"`
	Revoked int             `json:"revoked"`
}

// revokeTokenResponse is the response for DELETE /tokens/:name.
type revokeTokenResponse struct {
	Message string `json:"message"`
}

// HandleCreateToken handles POST /api/v1/tokens.
func (h *Handler) HandleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	// Validate required fields
	var details []types.ValidationDetail
	if req.Name == "" {
		details = append(details, types.ValidationDetail{Field: "name", Message: "required"})
	}
	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}

	// Check for existing active token with same name
	existing, err := h.tokens.GetTokenByName(r.Context(), req.Name)
	if err == nil && existing != nil && existing.RevokedAt == "" {
		WriteError(w, http.StatusConflict, "Conflict",
			fmt.Sprintf("Active token with name '%s' already exists", req.Name))
		return
	}

	// Generate and store token
	tokenValue, err := h.tokens.GenerateToken()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "Failed to generate token")
		return
	}

	if err := h.tokens.CreateToken(r.Context(), req.Name, tokenValue); err != nil {
		// Handle unique constraint violation (name already exists but revoked, then re-inserted)
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			WriteError(w, http.StatusConflict, "Conflict",
				fmt.Sprintf("Active token with name '%s' already exists", req.Name))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Fetch the created token to get created_at timestamp
	created, err := h.tokens.GetTokenByName(r.Context(), req.Name)
	if err != nil {
		// Token was created but we can't fetch it — return what we have
		WriteJSON(w, http.StatusCreated, createTokenResponse{
			Name:  req.Name,
			Token: tokenValue,
		})
		return
	}

	WriteJSON(w, http.StatusCreated, createTokenResponse{
		Name:      created.Name,
		Token:     tokenValue,
		CreatedAt: created.CreatedAt,
	})
}

// HandleListTokens handles GET /api/v1/tokens.
func (h *Handler) HandleListTokens(w http.ResponseWriter, r *http.Request) {
	// Always include revoked tokens so we can show status
	tokens, err := h.tokens.ListTokens(r.Context(), true)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	var items []tokenListItem
	active := 0
	revoked := 0

	for _, t := range tokens {
		status := "active"
		if t.RevokedAt != "" {
			status = "revoked"
			revoked++
		} else {
			active++
		}

		items = append(items, tokenListItem{
			Name:        t.Name,
			TokenPrefix: t.Token, // Already masked to 8-char prefix by storage
			CreatedAt:   t.CreatedAt,
			LastUsed:    t.LastUsed,
			Status:      status,
		})
	}

	if items == nil {
		items = []tokenListItem{}
	}

	WriteJSON(w, http.StatusOK, listTokensResponse{
		Tokens:  items,
		Active:  active,
		Revoked: revoked,
	})
}

// HandleRevokeToken handles DELETE /api/v1/tokens/:name.
func (h *Handler) HandleRevokeToken(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if err := h.tokens.RevokeToken(r.Context(), name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, http.StatusNotFound, "Not Found",
				fmt.Sprintf("Token '%s' not found", name))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, revokeTokenResponse{
		Message: fmt.Sprintf("Token '%s' revoked", name),
	})
}
