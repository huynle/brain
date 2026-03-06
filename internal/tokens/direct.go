package tokens

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/huynle/brain-api/internal/storage"
)

// openDatabase opens the database connection at brainDir/.zk/brain.db
func openDatabase(brainDir string) (*storage.StorageLayer, error) {
	dbPath := filepath.Join(brainDir, ".zk", "brain.db")
	store, err := storage.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return store, nil
}

// CreateTokenDirect creates a token by directly accessing the database.
// Used for bootstrap scenarios when the API server is not running.
func CreateTokenDirect(brainDir, name string) (*storage.Token, error) {
	store, err := openDatabase(brainDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// Generate token
	tokenStr, err := store.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	// Create token in database
	ctx := context.Background()
	if err := store.CreateToken(ctx, name, tokenStr); err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}

	// Get the created token to return with timestamp
	token, err := store.GetTokenByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get created token: %w", err)
	}

	return token, nil
}

// ListTokensDirect lists all tokens by directly accessing the database.
// Used for bootstrap scenarios when the API server is not running.
func ListTokensDirect(brainDir string) ([]storage.Token, error) {
	store, err := openDatabase(brainDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// List all tokens
	ctx := context.Background()
	tokens, err := store.ListTokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}

	return tokens, nil
}

// RevokeTokenDirect revokes a token by directly accessing the database.
// Used for bootstrap scenarios when the API server is not running.
func RevokeTokenDirect(brainDir, name string) error {
	store, err := openDatabase(brainDir)
	if err != nil {
		return err
	}
	defer store.Close()

	// Delete the token
	ctx := context.Background()
	if err := store.DeleteToken(ctx, name); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}

	return nil
}
