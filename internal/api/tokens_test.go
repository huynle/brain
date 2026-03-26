package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/storage"
)

// =============================================================================
// Mock TokenService
// =============================================================================

type mockTokenService struct {
	generateTokenFunc  func() (string, error)
	createTokenFunc    func(ctx context.Context, name, token string) error
	listTokensFunc     func(ctx context.Context, includeRevoked ...bool) ([]storage.Token, error)
	getTokenByNameFunc func(ctx context.Context, name string) (*storage.Token, error)
	revokeTokenFunc    func(ctx context.Context, name string) error
}

func (m *mockTokenService) GenerateToken() (string, error) {
	if m.generateTokenFunc != nil {
		return m.generateTokenFunc()
	}
	return "test-token-value-1234567890abcdef1234567890abcdef12345678901", nil
}

func (m *mockTokenService) CreateToken(ctx context.Context, name, token string) error {
	if m.createTokenFunc != nil {
		return m.createTokenFunc(ctx, name, token)
	}
	return nil
}

func (m *mockTokenService) ListTokens(ctx context.Context, includeRevoked ...bool) ([]storage.Token, error) {
	if m.listTokensFunc != nil {
		return m.listTokensFunc(ctx, includeRevoked...)
	}
	return []storage.Token{}, nil
}

func (m *mockTokenService) GetTokenByName(ctx context.Context, name string) (*storage.Token, error) {
	if m.getTokenByNameFunc != nil {
		return m.getTokenByNameFunc(ctx, name)
	}
	return nil, fmt.Errorf("token not found: %s", name)
}

func (m *mockTokenService) RevokeToken(ctx context.Context, name string) error {
	if m.revokeTokenFunc != nil {
		return m.revokeTokenFunc(ctx, name)
	}
	return nil
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTokenTestRouter(mock *mockTokenService) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithTokenService(mock))
	r := chi.NewRouter()
	r.Route("/tokens", func(r chi.Router) {
		r.Post("/", h.HandleCreateToken)
		r.Get("/", h.HandleListTokens)
		r.Delete("/{name}", h.HandleRevokeToken)
	})
	return r
}

// =============================================================================
// Create Token Tests
// =============================================================================

func TestHandleCreateToken(t *testing.T) {
	tests := []struct {
		name          string
		body          any
		mockGenerate  func() (string, error)
		mockCreate    func(ctx context.Context, name, token string) error
		mockGetByName func(ctx context.Context, name string) (*storage.Token, error)
		wantStatus    int
		checkBody     func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			body: map[string]any{"name": "runner-prod"},
			mockGetByName: func(_ context.Context, name string) (*storage.Token, error) {
				// First call: not found (no existing), Second call: return created
				return nil, fmt.Errorf("token not found: %s", name)
			},
			mockCreate: func(_ context.Context, name, token string) error {
				return nil
			},
			wantStatus: http.StatusCreated,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[createTokenResponse](t, resp)
				if body.Name != "runner-prod" {
					t.Errorf("expected name runner-prod, got %s", body.Name)
				}
				if body.Token == "" {
					t.Error("expected token value, got empty")
				}
			},
		},
		{
			name:       "missing name",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       nil, // signals to send raw invalid JSON
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "duplicate active name",
			body: map[string]any{"name": "runner-prod"},
			mockGetByName: func(_ context.Context, name string) (*storage.Token, error) {
				return &storage.Token{
					Name:      name,
					Token:     "existing-token",
					CreatedAt: "2025-01-01T00:00:00Z",
					RevokedAt: "", // active
				}, nil
			},
			wantStatus: http.StatusConflict,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[map[string]any](t, resp)
				msg, ok := body["message"].(string)
				if !ok || msg == "" {
					t.Error("expected error message")
				}
			},
		},
		{
			name: "revoked name allows re-creation",
			body: map[string]any{"name": "runner-old"},
			mockGetByName: func() func(context.Context, string) (*storage.Token, error) {
				calls := 0
				return func(_ context.Context, name string) (*storage.Token, error) {
					calls++
					if calls == 1 {
						// First call: check existing — found but revoked
						return &storage.Token{
							Name:      name,
							Token:     "old-token",
							RevokedAt: "2025-01-01T00:00:00Z",
						}, nil
					}
					// Second call: fetch created token
					return &storage.Token{
						Name:      name,
						Token:     "new-token-value",
						CreatedAt: "2025-06-01T00:00:00Z",
					}, nil
				}
			}(),
			wantStatus: http.StatusCreated,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[createTokenResponse](t, resp)
				if body.Name != "runner-old" {
					t.Errorf("expected name runner-old, got %s", body.Name)
				}
			},
		},
		{
			name: "generate token error",
			body: map[string]any{"name": "test"},
			mockGetByName: func(_ context.Context, name string) (*storage.Token, error) {
				return nil, fmt.Errorf("token not found: %s", name)
			},
			mockGenerate: func() (string, error) {
				return "", fmt.Errorf("crypto error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTokenService{
				generateTokenFunc:  tt.mockGenerate,
				createTokenFunc:    tt.mockCreate,
				getTokenByNameFunc: tt.mockGetByName,
			}
			router := newTokenTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var resp *http.Response
			var err error
			if tt.body == nil {
				// Send invalid JSON
				resp, err = http.Post(srv.URL+"/tokens", "application/json",
					strings.NewReader("not valid json{{{"))
			} else {
				resp, err = http.Post(srv.URL+"/tokens", "application/json",
					jsonBody(t, tt.body))
			}
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// List Tokens Tests
// =============================================================================

func TestHandleListTokens(t *testing.T) {
	tests := []struct {
		name       string
		mockList   func(ctx context.Context, includeRevoked ...bool) ([]storage.Token, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "empty list",
			mockList: func(_ context.Context, _ ...bool) ([]storage.Token, error) {
				return []storage.Token{}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[listTokensResponse](t, resp)
				if len(body.Tokens) != 0 {
					t.Errorf("expected 0 tokens, got %d", len(body.Tokens))
				}
				if body.Active != 0 {
					t.Errorf("expected 0 active, got %d", body.Active)
				}
			},
		},
		{
			name: "mixed active and revoked",
			mockList: func(_ context.Context, _ ...bool) ([]storage.Token, error) {
				return []storage.Token{
					{Name: "prod", Token: "abc12def", CreatedAt: "2025-01-01", LastUsed: "2025-06-01"},
					{Name: "staging", Token: "xyz98765", CreatedAt: "2025-02-01"},
					{Name: "old", Token: "old12345", CreatedAt: "2024-01-01", RevokedAt: "2025-01-01"},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[listTokensResponse](t, resp)
				if len(body.Tokens) != 3 {
					t.Fatalf("expected 3 tokens, got %d", len(body.Tokens))
				}
				if body.Active != 2 {
					t.Errorf("expected 2 active, got %d", body.Active)
				}
				if body.Revoked != 1 {
					t.Errorf("expected 1 revoked, got %d", body.Revoked)
				}

				// Check first token has prefix, not full value
				if body.Tokens[0].TokenPrefix != "abc12def" {
					t.Errorf("expected prefix abc12def, got %s", body.Tokens[0].TokenPrefix)
				}
				if body.Tokens[0].Status != "active" {
					t.Errorf("expected status active, got %s", body.Tokens[0].Status)
				}
				if body.Tokens[2].Status != "revoked" {
					t.Errorf("expected status revoked, got %s", body.Tokens[2].Status)
				}
			},
		},
		{
			name: "storage error",
			mockList: func(_ context.Context, _ ...bool) ([]storage.Token, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTokenService{listTokensFunc: tt.mockList}
			router := newTokenTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/tokens")
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Revoke Token Tests
// =============================================================================

func TestHandleRevokeToken(t *testing.T) {
	tests := []struct {
		name       string
		tokenName  string
		mockRevoke func(ctx context.Context, name string) error
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:      "success",
			tokenName: "runner-prod",
			mockRevoke: func(_ context.Context, name string) error {
				return nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[revokeTokenResponse](t, resp)
				if body.Message != "Token 'runner-prod' revoked" {
					t.Errorf("unexpected message: %s", body.Message)
				}
			},
		},
		{
			name:      "not found",
			tokenName: "nonexistent",
			mockRevoke: func(_ context.Context, name string) error {
				return fmt.Errorf("token not found: %s", name)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:      "storage error",
			tokenName: "test",
			mockRevoke: func(_ context.Context, name string) error {
				return fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTokenService{revokeTokenFunc: tt.mockRevoke}
			router := newTokenTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			req, err := http.NewRequest(http.MethodDelete, srv.URL+"/tokens/"+tt.tokenName, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}
