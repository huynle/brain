package tokens

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/huynle/brain-api/internal/storage"
)

// setupTestDB creates a temporary test database and returns the brainDir path and cleanup function.
func setupTestDB(t *testing.T) (string, func()) {
	t.Helper()

	// Create temporary directory for test brain
	tmpDir, err := os.MkdirTemp("", "brain-tokens-test-*")
	require.NoError(t, err)

	// Create .zk directory
	zkDir := filepath.Join(tmpDir, ".zk")
	err = os.MkdirAll(zkDir, 0755)
	require.NoError(t, err)

	// Initialize database
	dbPath := filepath.Join(zkDir, "brain.db")
	store, err := storage.New(dbPath)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup
}

func TestCreateTokenDirect(t *testing.T) {
	brainDir, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a token
	token, err := CreateTokenDirect(brainDir, "test-token")
	require.NoError(t, err)
	assert.NotNil(t, token)
	assert.Equal(t, "test-token", token.Name)
	assert.NotEmpty(t, token.Token)
	assert.NotEmpty(t, token.CreatedAt)
	assert.Len(t, token.Token, 43) // base64 URL-encoded 32 bytes = 43 chars

	// Verify token exists in database
	dbPath := filepath.Join(brainDir, ".zk", "brain.db")
	store, err := storage.New(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	stored, err := store.GetTokenByName(ctx, "test-token")
	require.NoError(t, err)
	assert.Equal(t, token.Token, stored.Token)
}

func TestCreateTokenDirect_Duplicate(t *testing.T) {
	brainDir, cleanup := setupTestDB(t)
	defer cleanup()

	// Create first token
	_, err := CreateTokenDirect(brainDir, "duplicate-token")
	require.NoError(t, err)

	// Attempt to create duplicate
	_, err = CreateTokenDirect(brainDir, "duplicate-token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "UNIQUE constraint")
}

func TestListTokensDirect(t *testing.T) {
	brainDir, cleanup := setupTestDB(t)
	defer cleanup()

	// Test with empty database
	tokens, err := ListTokensDirect(brainDir)
	require.NoError(t, err)
	assert.Empty(t, tokens)

	// Add some tokens
	_, err = CreateTokenDirect(brainDir, "token-1")
	require.NoError(t, err)
	_, err = CreateTokenDirect(brainDir, "token-2")
	require.NoError(t, err)

	// List tokens
	tokens, err = ListTokensDirect(brainDir)
	require.NoError(t, err)
	assert.Len(t, tokens, 2)

	// Check that both tokens are present (order may vary)
	names := []string{tokens[0].Name, tokens[1].Name}
	assert.Contains(t, names, "token-1")
	assert.Contains(t, names, "token-2")
}

func TestRevokeTokenDirect(t *testing.T) {
	brainDir, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a token
	_, err := CreateTokenDirect(brainDir, "revoke-me")
	require.NoError(t, err)

	// Revoke it
	err = RevokeTokenDirect(brainDir, "revoke-me")
	require.NoError(t, err)

	// Verify it's gone
	tokens, err := ListTokensDirect(brainDir)
	require.NoError(t, err)
	assert.Empty(t, tokens)
}

func TestRevokeTokenDirect_NotFound(t *testing.T) {
	brainDir, cleanup := setupTestDB(t)
	defer cleanup()

	// Attempt to revoke non-existent token
	err := RevokeTokenDirect(brainDir, "does-not-exist")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token not found")
}
