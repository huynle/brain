package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
)

// setupTestDB creates a temporary database for testing
func setupTestDB(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	brainDir := filepath.Join(tmpDir, "brain")
	zkDir := filepath.Join(brainDir, ".zk")
	if err := os.MkdirAll(zkDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Initialize database with schema
	dbPath := filepath.Join(zkDir, "brain.db")
	store, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("storage.New failed: %v", err)
	}
	defer store.Close()

	return brainDir
}

// newDirectOnlyConfig creates a UnifiedConfig that forces direct DB access
// by pointing the API URL at an unreachable address, preventing interference
// from any real server running on localhost.
func newDirectOnlyConfig(brainDir string) *UnifiedConfig {
	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = brainDir
	// Point at a non-routable address to ensure API health check fails fast
	cfg.Runner.BrainAPIURL = "http://192.0.2.1:1"
	return cfg
}

func TestTokenCommand_Type(t *testing.T) {
	cmd := &TokenCommand{}
	if got := cmd.Type(); got != "token" {
		t.Errorf("Type() = %q, want %q", got, "token")
	}
}

func TestTokenCommand_CreateToken(t *testing.T) {
	brainDir := setupTestDB(t)

	cmd := &TokenCommand{
		Subcommand: "create",
		Name:       "test-token",
		Config:     newDirectOnlyConfig(brainDir),
		httpClient: &http.Client{Timeout: 100 * time.Millisecond},
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	// Verify token was created in database
	store, err := storage.New(filepath.Join(brainDir, ".zk", "brain.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	token, err := store.GetTokenByName(ctx, "test-token")
	if err != nil {
		t.Fatalf("GetTokenByName failed: %v", err)
	}

	if token.Name != "test-token" {
		t.Errorf("token name = %q, want %q", token.Name, "test-token")
	}
	if token.Token == "" {
		t.Error("token string is empty")
	}
}

func TestTokenCommand_ListTokens(t *testing.T) {
	brainDir := setupTestDB(t)

	// Create some test tokens directly
	store, err := storage.New(filepath.Join(brainDir, ".zk", "brain.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	ctx := context.Background()

	token1, _ := store.GenerateToken()
	store.CreateToken(ctx, "token1", token1)
	token2, _ := store.GenerateToken()
	store.CreateToken(ctx, "token2", token2)
	store.Close()

	// Now list them via command
	cmd := &TokenCommand{
		Subcommand: "list",
		Config:     newDirectOnlyConfig(brainDir),
		httpClient: &http.Client{Timeout: 100 * time.Millisecond},
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	// Test passes if no error - output formatting tested manually
}

func TestTokenCommand_RevokeToken(t *testing.T) {
	brainDir := setupTestDB(t)

	// Create a token first
	store, err := storage.New(filepath.Join(brainDir, ".zk", "brain.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	ctx := context.Background()
	token, _ := store.GenerateToken()
	store.CreateToken(ctx, "revoke-me", token)
	store.Close()

	// Revoke it via command
	cmd := &TokenCommand{
		Subcommand: "revoke",
		Name:       "revoke-me",
		Config:     newDirectOnlyConfig(brainDir),
		httpClient: &http.Client{Timeout: 100 * time.Millisecond},
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	// Verify it's soft-revoked (still exists but has revoked_at set)
	store, _ = storage.New(filepath.Join(brainDir, ".zk", "brain.db"))
	defer store.Close()
	retrieved, err := store.GetTokenByName(ctx, "revoke-me")
	if err != nil {
		t.Fatalf("GetTokenByName after revoke should still find token: %v", err)
	}
	if retrieved.RevokedAt == "" {
		t.Error("expected RevokedAt to be set after revoke")
	}

	// Verify it's excluded from default list
	tokens, err := store.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens failed: %v", err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens in default list after revoke, got %d", len(tokens))
	}
}

func TestTokenCommand_CreateWithoutName(t *testing.T) {
	brainDir := setupTestDB(t)

	cmd := &TokenCommand{
		Subcommand: "create",
		Name:       "", // empty name
		Config:     newDirectOnlyConfig(brainDir),
		httpClient: &http.Client{Timeout: 100 * time.Millisecond},
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error for empty name, got nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("expected error about 'name', got: %v", err)
	}
}

func TestTokenCommand_RevokeNonexistent(t *testing.T) {
	brainDir := setupTestDB(t)

	cmd := &TokenCommand{
		Subcommand: "revoke",
		Name:       "does-not-exist",
		Config:     newDirectOnlyConfig(brainDir),
		httpClient: &http.Client{Timeout: 100 * time.Millisecond},
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error for nonexistent token, got nil")
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "normal token",
			token: "abcdefgh12345678901234567890123456789012",
			want:  "abcdefgh...9012",
		},
		{
			name:  "short token",
			token: "short",
			want:  "short",
		},
		{
			name:  "exact 12 chars",
			token: "123456789012",
			want:  "123456789012",
		},
		{
			name:  "13 chars - should mask",
			token: "1234567890123",
			want:  "12345678...0123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskToken(tt.token)
			if got != tt.want {
				t.Errorf("maskToken(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

// =============================================================================
// API Routing Tests
// =============================================================================

// newMockAPIServer creates a mock Brain API server for testing.
// It handles /api/v1/health, /api/v1/tokens endpoints.
func newMockAPIServer(t *testing.T) *httptest.Server {
	t.Helper()

	// In-memory token store for the mock
	type mockToken struct {
		Name      string `json:"name"`
		Token     string `json:"token"`
		CreatedAt string `json:"created_at"`
		Status    string `json:"status"`
	}
	tokenStore := make(map[string]*mockToken)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	mux.HandleFunc("/api/v1/tokens", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Bad Request", "message": "Invalid JSON"})
				return
			}
			if req.Name == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Validation Error", "message": "name is required"})
				return
			}
			if _, exists := tokenStore[req.Name]; exists {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{"error": "Conflict", "message": "Active token with name '" + req.Name + "' already exists"})
				return
			}
			tok := &mockToken{
				Name:      req.Name,
				Token:     "brn_mock_token_value_1234567890abcdef",
				CreatedAt: "2026-01-01 00:00:00",
				Status:    "active",
			}
			tokenStore[req.Name] = tok
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"name":       tok.Name,
				"token":      tok.Token,
				"created_at": tok.CreatedAt,
			})

		case http.MethodGet:
			var items []map[string]string
			active, revoked := 0, 0
			for _, tok := range tokenStore {
				status := tok.Status
				if status == "" {
					status = "active"
				}
				if status == "active" {
					active++
				} else {
					revoked++
				}
				items = append(items, map[string]string{
					"name":         tok.Name,
					"token_prefix": tok.Token[:8],
					"created_at":   tok.CreatedAt,
					"status":       status,
				})
			}
			if items == nil {
				items = []map[string]string{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"tokens":  items,
				"active":  active,
				"revoked": revoked,
			})

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// Handle DELETE /api/v1/tokens/{name}
	mux.HandleFunc("/api/v1/tokens/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Extract name from path: /api/v1/tokens/{name}
		name := strings.TrimPrefix(r.URL.Path, "/api/v1/tokens/")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tok, exists := tokenStore[name]
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Not Found", "message": "Token '" + name + "' not found"})
			return
		}
		tok.Status = "revoked"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Token '" + name + "' revoked"})
	})

	return httptest.NewServer(mux)
}

// newTokenCommandWithAPI creates a TokenCommand configured to use the given API server.
func newTokenCommandWithAPI(t *testing.T, server *httptest.Server, subcommand, name string) *TokenCommand {
	t.Helper()
	cfg := &UnifiedConfig{}
	cfg.Runner.BrainAPIURL = server.URL
	cfg.Runner.APIToken = "test-bearer-token"
	return &TokenCommand{
		Subcommand: subcommand,
		Name:       name,
		Config:     cfg,
		httpClient: server.Client(),
	}
}

func TestTokenCommand_IsAPIAvailable(t *testing.T) {
	t.Run("returns true when API is healthy", func(t *testing.T) {
		server := newMockAPIServer(t)
		defer server.Close()

		cmd := newTokenCommandWithAPI(t, server, "list", "")
		if !cmd.isAPIAvailable() {
			t.Error("isAPIAvailable() = false, want true")
		}
	})

	t.Run("returns false when API is down", func(t *testing.T) {
		// Use a closed server
		server := newMockAPIServer(t)
		server.Close()

		cmd := newTokenCommandWithAPI(t, server, "list", "")
		if cmd.isAPIAvailable() {
			t.Error("isAPIAvailable() = true, want false")
		}
	})
}

func TestTokenCommand_CreateTokenViaAPI(t *testing.T) {
	server := newMockAPIServer(t)
	defer server.Close()

	cmd := newTokenCommandWithAPI(t, server, "create", "my-api-token")
	// Need a brainDir for Execute, but API should be preferred
	brainDir := setupTestDB(t)
	cmd.Config.Server.BrainDir = brainDir

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

func TestTokenCommand_CreateTokenViaAPI_Conflict(t *testing.T) {
	server := newMockAPIServer(t)
	defer server.Close()

	// Create first token
	cmd := newTokenCommandWithAPI(t, server, "create", "dup-token")
	cmd.Config.Server.BrainDir = setupTestDB(t)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// Try to create duplicate
	cmd2 := newTokenCommandWithAPI(t, server, "create", "dup-token")
	cmd2.Config.Server.BrainDir = cmd.Config.Server.BrainDir
	err := cmd2.Execute()
	if err == nil {
		t.Fatal("expected error for duplicate token, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestTokenCommand_ListTokensViaAPI(t *testing.T) {
	server := newMockAPIServer(t)
	defer server.Close()

	// Create a token first
	cmd := newTokenCommandWithAPI(t, server, "create", "list-test-token")
	cmd.Config.Server.BrainDir = setupTestDB(t)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// List tokens
	listCmd := newTokenCommandWithAPI(t, server, "list", "")
	listCmd.Config.Server.BrainDir = cmd.Config.Server.BrainDir

	err := listCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() list failed: %v", err)
	}
}

func TestTokenCommand_ListTokensViaAPI_Empty(t *testing.T) {
	server := newMockAPIServer(t)
	defer server.Close()

	cmd := newTokenCommandWithAPI(t, server, "list", "")
	cmd.Config.Server.BrainDir = setupTestDB(t)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() list empty failed: %v", err)
	}
}

func TestTokenCommand_RevokeTokenViaAPI(t *testing.T) {
	server := newMockAPIServer(t)
	defer server.Close()

	// Create a token first
	cmd := newTokenCommandWithAPI(t, server, "create", "revoke-api-token")
	cmd.Config.Server.BrainDir = setupTestDB(t)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Revoke it
	revokeCmd := newTokenCommandWithAPI(t, server, "revoke", "revoke-api-token")
	revokeCmd.Config.Server.BrainDir = cmd.Config.Server.BrainDir

	err := revokeCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() revoke failed: %v", err)
	}
}

func TestTokenCommand_RevokeTokenViaAPI_NotFound(t *testing.T) {
	server := newMockAPIServer(t)
	defer server.Close()

	cmd := newTokenCommandWithAPI(t, server, "revoke", "nonexistent-token")
	cmd.Config.Server.BrainDir = setupTestDB(t)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestTokenCommand_FallbackToDirect(t *testing.T) {
	// Use a closed server to simulate API being down
	server := newMockAPIServer(t)
	server.Close()

	brainDir := setupTestDB(t)

	t.Run("create falls back to direct DB", func(t *testing.T) {
		cmd := newTokenCommandWithAPI(t, server, "create", "fallback-token")
		cmd.Config.Server.BrainDir = brainDir

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		// Verify token was created in DB
		store, err := storage.New(filepath.Join(brainDir, ".zk", "brain.db"))
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
		defer store.Close()

		ctx := context.Background()
		tok, err := store.GetTokenByName(ctx, "fallback-token")
		if err != nil {
			t.Fatalf("GetTokenByName failed: %v", err)
		}
		if tok.Name != "fallback-token" {
			t.Errorf("token name = %q, want %q", tok.Name, "fallback-token")
		}
	})

	t.Run("list falls back to direct DB", func(t *testing.T) {
		cmd := newTokenCommandWithAPI(t, server, "list", "")
		cmd.Config.Server.BrainDir = brainDir

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}
	})

	t.Run("revoke falls back to direct DB", func(t *testing.T) {
		cmd := newTokenCommandWithAPI(t, server, "revoke", "fallback-token")
		cmd.Config.Server.BrainDir = brainDir

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}
	})
}

func TestTokenCommand_APIURLDefault(t *testing.T) {
	cmd := &TokenCommand{Config: &UnifiedConfig{}}
	got := cmd.apiURL()
	if got != "http://localhost:3333" {
		t.Errorf("apiURL() = %q, want %q", got, "http://localhost:3333")
	}
}

func TestTokenCommand_APIURLFromConfig(t *testing.T) {
	cmd := &TokenCommand{Config: &UnifiedConfig{}}
	cmd.Config.Runner.BrainAPIURL = "http://custom:9999"
	got := cmd.apiURL()
	if got != "http://custom:9999" {
		t.Errorf("apiURL() = %q, want %q", got, "http://custom:9999")
	}
}

func TestTokenCommand_BearerAuth(t *testing.T) {
	// Verify that API requests include the Bearer token
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/api/v1/health" {
			json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
			return
		}
		// Return empty list for tokens
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tokens":  []interface{}{},
			"active":  0,
			"revoked": 0,
		})
	}))
	defer server.Close()

	cmd := &TokenCommand{
		Subcommand: "list",
		Config:     &UnifiedConfig{},
		httpClient: server.Client(),
	}
	cmd.Config.Runner.BrainAPIURL = server.URL
	cmd.Config.Runner.APIToken = "my-secret-token"
	cmd.Config.Server.BrainDir = setupTestDB(t)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	if gotAuth != "Bearer my-secret-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer my-secret-token")
	}
}
