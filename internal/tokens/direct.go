package tokens

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

// openDatabase opens the database connection at brainDir/.brain-data/brain.db
func openDatabase(brainDir string) (*storage.TokenAdmin, func(), error) {
	// Offline commands are host-owner operations, not bearer-token requests.
	// Refuse multi until offline tenant/operator lifecycle support is implemented.
	cfg := config.Load()
	if err := cfg.Err(); err != nil {
		return nil, nil, err
	}
	mode, err := tenant.ParseMode(string(cfg.Tenancy.Mode))
	if err != nil || mode != tenant.ModeSingle {
		return nil, nil, fmt.Errorf("offline tokens require single mode")
	}
	dbPath := filepath.Join(brainDir, config.DataDir, "brain.db")
	owner, err := storage.New(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	cleanup := func() { _ = owner.Close() }
	control, err := owner.Control()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	operator, err := auth.AuthenticateLocalDatabaseOwner(dbPath)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	admin, err := control.TokenAdmin(operator)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return admin, cleanup, nil
}

// CreateTokenDirect creates a token by directly accessing the database.
// Used for bootstrap scenarios when the API server is not running.
// If scope is empty, it defaults to "admin:*".
func CreateTokenDirect(brainDir, name, scope string) (*storage.Token, error) {
	store, cleanup, err := openDatabase(brainDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Generate token
	tokenStr, err := store.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	// Create token in database
	ctx := context.Background()
	if err := store.CreateToken(ctx, name, tokenStr, scope); err != nil {
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
	store, cleanup, err := openDatabase(brainDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

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
	store, cleanup, err := openDatabase(brainDir)
	if err != nil {
		return err
	}
	defer cleanup()

	// Soft-revoke the token (sets revoked_at instead of deleting)
	ctx := context.Background()
	if err := store.RevokeToken(ctx, name); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	return nil
}
