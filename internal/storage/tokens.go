package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
)

// GenerateToken generates a secure random token using 32 bytes of crypto/rand
// and encodes it as base64 URL-safe string (43 characters).
func (s *StorageLayer) GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b), nil
}

// CreateToken stores a token with the given name and scope in the database.
// If scope is empty, it defaults to "admin:*".
func (s *StorageLayer) CreateToken(ctx context.Context, name, token, scope string) error {
	if scope == "" {
		scope = "admin:*"
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO api_tokens (name, token, scope) VALUES (?, ?, ?)",
		name, token, scope,
	)
	if err != nil {
		return fmt.Errorf("insert token: %w", err)
	}
	return nil
}

// Token represents an API token.
type Token struct {
	Name      string
	Token     string // Full value on create/validate; prefix (8 chars) on list
	Scope     string // Token scope: "admin:*", "runner:*", or "read:*"
	CreatedAt string
	LastUsed  string
	RevokedAt string
}

// tokenPrefix returns the first 8 characters of a token value for safe display.
func tokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

// ValidateToken looks up a token by its value, rejects revoked tokens,
// and updates last_used on success. Returns the full Token on success.
func (s *StorageLayer) ValidateToken(ctx context.Context, tokenValue string) (*Token, error) {
	var t Token
	err := s.db.QueryRowContext(ctx,
		`SELECT name, token, scope, created_at, COALESCE(last_used, '') 
         FROM api_tokens WHERE token = ? AND revoked_at IS NULL`,
		tokenValue,
	).Scan(&t.Name, &t.Token, &t.Scope, &t.CreatedAt, &t.LastUsed)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("token not found or revoked")
	}
	if err != nil {
		return nil, fmt.Errorf("validate token: %w", err)
	}

	// Update last_used asynchronously
	go s.UpdateTokenLastUsed(context.Background(), t.Name)

	return &t, nil
}

// ListTokens returns non-revoked tokens by default. Token values are masked
// to show only the first 8 characters. Set includeRevoked to true to include
// revoked tokens in the result.
func (s *StorageLayer) ListTokens(ctx context.Context, includeRevoked ...bool) ([]Token, error) {
	query := "SELECT name, token, scope, created_at, COALESCE(last_used, ''), COALESCE(revoked_at, '') FROM api_tokens"
	if len(includeRevoked) == 0 || !includeRevoked[0] {
		query += " WHERE revoked_at IS NULL"
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query tokens: %w", err)
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.Name, &t.Token, &t.Scope, &t.CreatedAt, &t.LastUsed, &t.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		// Mask token value to prefix only
		t.Token = tokenPrefix(t.Token)
		tokens = append(tokens, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if tokens == nil {
		return []Token{}, nil
	}
	return tokens, nil
}

// GetTokenByName returns a token by name.
func (s *StorageLayer) GetTokenByName(ctx context.Context, name string) (*Token, error) {
	var t Token
	err := s.db.QueryRowContext(ctx,
		"SELECT name, token, scope, created_at, COALESCE(last_used, ''), COALESCE(revoked_at, '') FROM api_tokens WHERE name = ?",
		name,
	).Scan(&t.Name, &t.Token, &t.Scope, &t.CreatedAt, &t.LastUsed, &t.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("token not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("query token: %w", err)
	}
	return &t, nil
}

// RevokeToken soft-revokes a token by setting revoked_at. The token remains
// in the database but will be rejected by ValidateToken and excluded from
// ListTokens by default.
func (s *StorageLayer) RevokeToken(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE api_tokens SET revoked_at = datetime('now') WHERE name = ? AND revoked_at IS NULL",
		name,
	)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("token not found: %s", name)
	}
	return nil
}

// DeleteTokenPermanent permanently removes a token from the database.
// For normal revocation, use RevokeToken instead.
func (s *StorageLayer) DeleteTokenPermanent(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM api_tokens WHERE name = ?",
		name,
	)
	if err != nil {
		return fmt.Errorf("delete token: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("token not found: %s", name)
	}
	return nil
}

// CountActiveTokens returns the number of non-revoked tokens in the database.
// Used by the bootstrap endpoint to determine if token creation should be allowed
// without authentication (only when zero tokens exist).
func (s *StorageLayer) CountActiveTokens(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM api_tokens WHERE revoked_at IS NULL",
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active tokens: %w", err)
	}
	return count, nil
}

// UpdateTokenLastUsed updates the last_used timestamp for a token.
func (s *StorageLayer) UpdateTokenLastUsed(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE api_tokens SET last_used = datetime('now') WHERE name = ?",
		name,
	)
	if err != nil {
		return fmt.Errorf("update token last_used: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("token not found: %s", name)
	}
	return nil
}
