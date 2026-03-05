package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		Config:     &UnifiedConfig{},
	}
	// Set brainDir in config
	cmd.Config.Server.BrainDir = brainDir

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
		Config:     &UnifiedConfig{},
	}
	cmd.Config.Server.BrainDir = brainDir

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
		Config:     &UnifiedConfig{},
	}
	cmd.Config.Server.BrainDir = brainDir

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	// Verify it's gone
	store, _ = storage.New(filepath.Join(brainDir, ".zk", "brain.db"))
	defer store.Close()
	_, err = store.GetTokenByName(ctx, "revoke-me")
	if err == nil {
		t.Error("expected error after revoke, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestTokenCommand_CreateWithoutName(t *testing.T) {
	brainDir := setupTestDB(t)

	cmd := &TokenCommand{
		Subcommand: "create",
		Name:       "", // empty name
		Config:     &UnifiedConfig{},
	}
	cmd.Config.Server.BrainDir = brainDir

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
		Config:     &UnifiedConfig{},
	}
	cmd.Config.Server.BrainDir = brainDir

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
