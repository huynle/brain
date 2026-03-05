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

// CreateToken stores a token with the given name in the database.
func (s *StorageLayer) CreateToken(ctx context.Context, name, token string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO api_tokens (name, token) VALUES (?, ?)",
		name, token,
	)
	if err != nil {
		return fmt.Errorf("insert token: %w", err)
	}
	return nil
}

// Token represents an API token.
type Token struct {
	Name      string
	Token     string
	CreatedAt string
	LastUsed  string
}

// ListTokens returns all tokens.
func (s *StorageLayer) ListTokens(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, token, created_at, COALESCE(last_used, '') FROM api_tokens ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("query tokens: %w", err)
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.Name, &t.Token, &t.CreatedAt, &t.LastUsed); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
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
		"SELECT name, token, created_at, COALESCE(last_used, '') FROM api_tokens WHERE name = ?",
		name,
	).Scan(&t.Name, &t.Token, &t.CreatedAt, &t.LastUsed)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("token not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("query token: %w", err)
	}
	return &t, nil
}

// DeleteToken deletes a token by name.
func (s *StorageLayer) DeleteToken(ctx context.Context, name string) error {
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
