package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

// OAuthClient represents a registered OAuth 2.0 client.
type OAuthClient struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret"`
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	CreatedAt               int64    `json:"created_at"`
}

// OAuthAuthCode represents an authorization code.
type OAuthAuthCode struct {
	Code                string `json:"code"`
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope,omitempty"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	UserID              string `json:"user_id,omitempty"`
	ExpiresAt           int64  `json:"expires_at"`
	CreatedAt           int64  `json:"created_at"`
}

// OAuthAccessToken represents an access token.
type OAuthAccessToken struct {
	Token     string `json:"token"`
	ClientID  string `json:"client_id"`
	Scope     string `json:"scope,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
	CreatedAt int64  `json:"created_at"`
}

// OAuthRefreshToken represents a refresh token.
type OAuthRefreshToken struct {
	Token     string `json:"token"`
	ClientID  string `json:"client_id"`
	Scope     string `json:"scope,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
	CreatedAt int64  `json:"created_at"`
}

// TTL defaults
const (
	AuthCodeTTL     = 10 * time.Minute
	AccessTokenTTL  = 1 * time.Hour
	RefreshTokenTTL = 7 * 24 * time.Hour
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// generateOAuthClientID generates a client ID with "brain_" prefix + 32 hex chars.
func generateOAuthClientID() (string, error) {
	b := make([]byte, 16) // 16 bytes = 32 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate client ID: %w", err)
	}
	return "brain_" + hex.EncodeToString(b), nil
}

// marshalJSON is a helper that marshals a value to JSON string.
func marshalJSON(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalJSON is a helper that unmarshals a JSON string to a slice.
func unmarshalJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}

// ---------------------------------------------------------------------------
// Client store
// ---------------------------------------------------------------------------

// CreateOAuthClient inserts a new OAuth client. If ClientID is empty, one is
// generated with the "brain_" prefix + 32 hex chars.
func (s *StorageLayer) CreateOAuthClient(ctx context.Context, client *OAuthClient) error {
	if client.ClientID == "" {
		id, err := generateOAuthClientID()
		if err != nil {
			return err
		}
		client.ClientID = id
	}

	if client.CreatedAt == 0 {
		client.CreatedAt = time.Now().Unix()
	}
	if client.TokenEndpointAuthMethod == "" {
		client.TokenEndpointAuthMethod = "client_secret_post"
	}

	redirectURIs, err := marshalJSON(client.RedirectURIs)
	if err != nil {
		return fmt.Errorf("marshal redirect_uris: %w", err)
	}
	grantTypes, err := marshalJSON(client.GrantTypes)
	if err != nil {
		return fmt.Errorf("marshal grant_types: %w", err)
	}
	responseTypes, err := marshalJSON(client.ResponseTypes)
	if err != nil {
		return fmt.Errorf("marshal response_types: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO oauth_clients
			(client_id, client_secret, redirect_uris, client_name, client_uri,
			 logo_uri, scope, grant_types, response_types, token_endpoint_auth_method, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		client.ClientID, client.ClientSecret, redirectURIs, client.ClientName,
		client.ClientURI, client.LogoURI, client.Scope, grantTypes,
		responseTypes, client.TokenEndpointAuthMethod, client.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert oauth client: %w", err)
	}
	return nil
}

// GetOAuthClient retrieves an OAuth client by client ID.
func (s *StorageLayer) GetOAuthClient(ctx context.Context, clientID string) (*OAuthClient, error) {
	var c OAuthClient
	var redirectURIs, grantTypes, responseTypes string
	var clientName, clientURI, logoURI, scope sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT client_id, client_secret, redirect_uris, client_name, client_uri,
		       logo_uri, scope, grant_types, response_types, token_endpoint_auth_method, created_at
		FROM oauth_clients WHERE client_id = ?`, clientID,
	).Scan(&c.ClientID, &c.ClientSecret, &redirectURIs, &clientName, &clientURI,
		&logoURI, &scope, &grantTypes, &responseTypes, &c.TokenEndpointAuthMethod, &c.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("oauth client not found: %s", clientID)
	}
	if err != nil {
		return nil, fmt.Errorf("query oauth client: %w", err)
	}

	c.ClientName = clientName.String
	c.ClientURI = clientURI.String
	c.LogoURI = logoURI.String
	c.Scope = scope.String

	if err := unmarshalJSON(redirectURIs, &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("unmarshal redirect_uris: %w", err)
	}
	if err := unmarshalJSON(grantTypes, &c.GrantTypes); err != nil {
		return nil, fmt.Errorf("unmarshal grant_types: %w", err)
	}
	if err := unmarshalJSON(responseTypes, &c.ResponseTypes); err != nil {
		return nil, fmt.Errorf("unmarshal response_types: %w", err)
	}

	return &c, nil
}

// ListOAuthClients returns all registered OAuth clients.
func (s *StorageLayer) ListOAuthClients(ctx context.Context) ([]OAuthClient, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT client_id, client_secret, redirect_uris, client_name, client_uri,
		       logo_uri, scope, grant_types, response_types, token_endpoint_auth_method, created_at
		FROM oauth_clients ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query oauth clients: %w", err)
	}
	defer rows.Close()

	var clients []OAuthClient
	for rows.Next() {
		var c OAuthClient
		var redirectURIs, grantTypes, responseTypes string
		var clientName, clientURI, logoURI, scope sql.NullString

		if err := rows.Scan(&c.ClientID, &c.ClientSecret, &redirectURIs, &clientName,
			&clientURI, &logoURI, &scope, &grantTypes, &responseTypes,
			&c.TokenEndpointAuthMethod, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan oauth client: %w", err)
		}

		c.ClientName = clientName.String
		c.ClientURI = clientURI.String
		c.LogoURI = logoURI.String
		c.Scope = scope.String

		if err := unmarshalJSON(redirectURIs, &c.RedirectURIs); err != nil {
			return nil, fmt.Errorf("unmarshal redirect_uris: %w", err)
		}
		if err := unmarshalJSON(grantTypes, &c.GrantTypes); err != nil {
			return nil, fmt.Errorf("unmarshal grant_types: %w", err)
		}
		if err := unmarshalJSON(responseTypes, &c.ResponseTypes); err != nil {
			return nil, fmt.Errorf("unmarshal response_types: %w", err)
		}

		clients = append(clients, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if clients == nil {
		return []OAuthClient{}, nil
	}
	return clients, nil
}

// ---------------------------------------------------------------------------
// Auth code store
// ---------------------------------------------------------------------------

// CreateAuthCode inserts a new authorization code.
func (s *StorageLayer) CreateAuthCode(ctx context.Context, code *OAuthAuthCode) error {
	if code.CreatedAt == 0 {
		code.CreatedAt = time.Now().Unix()
	}
	if code.ExpiresAt == 0 {
		code.ExpiresAt = time.Now().Add(AuthCodeTTL).Unix()
	}
	if code.CodeChallengeMethod == "" {
		code.CodeChallengeMethod = "S256"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO oauth_auth_codes
			(code, client_id, redirect_uri, scope, code_challenge, code_challenge_method,
			 user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		code.Code, code.ClientID, code.RedirectURI, code.Scope,
		code.CodeChallenge, code.CodeChallengeMethod, code.UserID,
		code.ExpiresAt, code.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert auth code: %w", err)
	}
	return nil
}

// ConsumeAuthCode retrieves and deletes an authorization code (single-use).
// Returns an error if the code is expired or not found.
func (s *StorageLayer) ConsumeAuthCode(ctx context.Context, codeValue string) (*OAuthAuthCode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var code OAuthAuthCode
	var scope, userID sql.NullString

	err = tx.QueryRowContext(ctx, `
		SELECT code, client_id, redirect_uri, scope, code_challenge, code_challenge_method,
		       user_id, expires_at, created_at
		FROM oauth_auth_codes WHERE code = ?`, codeValue,
	).Scan(&code.Code, &code.ClientID, &code.RedirectURI, &scope,
		&code.CodeChallenge, &code.CodeChallengeMethod, &userID,
		&code.ExpiresAt, &code.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("auth code not found: %s", codeValue)
	}
	if err != nil {
		return nil, fmt.Errorf("query auth code: %w", err)
	}

	code.Scope = scope.String
	code.UserID = userID.String

	// Check expiry
	if time.Now().Unix() > code.ExpiresAt {
		// Delete the expired code
		_, _ = tx.ExecContext(ctx, "DELETE FROM oauth_auth_codes WHERE code = ?", codeValue)
		_ = tx.Commit()
		return nil, fmt.Errorf("auth code expired: %s", codeValue)
	}

	// Delete (single-use)
	_, err = tx.ExecContext(ctx, "DELETE FROM oauth_auth_codes WHERE code = ?", codeValue)
	if err != nil {
		return nil, fmt.Errorf("delete auth code: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &code, nil
}

// CleanupExpiredCodes removes all authorization codes past their expires_at.
func (s *StorageLayer) CleanupExpiredCodes(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM oauth_auth_codes WHERE expires_at < ?", time.Now().Unix())
	if err != nil {
		return fmt.Errorf("cleanup expired codes: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Access token store
// ---------------------------------------------------------------------------

// CreateAccessToken inserts a new access token.
func (s *StorageLayer) CreateAccessToken(ctx context.Context, token *OAuthAccessToken) error {
	if token.CreatedAt == 0 {
		token.CreatedAt = time.Now().Unix()
	}
	if token.ExpiresAt == 0 {
		token.ExpiresAt = time.Now().Add(AccessTokenTTL).Unix()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO oauth_access_tokens
			(token, client_id, scope, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		token.Token, token.ClientID, token.Scope, token.UserID,
		token.ExpiresAt, token.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert access token: %w", err)
	}
	return nil
}

// SaveAccessToken is a convenience method that persists an OAuth access token.
// Satisfies the oauth.AccessTokenStore interface.
func (s *StorageLayer) SaveAccessToken(ctx context.Context, token, clientID, scope string, expiresAt int64) error {
	return s.CreateAccessToken(ctx, &OAuthAccessToken{
		Token:     token,
		ClientID:  clientID,
		Scope:     scope,
		ExpiresAt: expiresAt,
	})
}

// GetAccessToken retrieves an access token and checks expiry.
// Returns an error if the token is expired or not found.
func (s *StorageLayer) GetAccessToken(ctx context.Context, tokenValue string) (*OAuthAccessToken, error) {
	var t OAuthAccessToken
	var scope, userID sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT token, client_id, scope, user_id, expires_at, created_at
		FROM oauth_access_tokens WHERE token = ?`, tokenValue,
	).Scan(&t.Token, &t.ClientID, &scope, &userID, &t.ExpiresAt, &t.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("access token not found: %s", tokenValue)
	}
	if err != nil {
		return nil, fmt.Errorf("query access token: %w", err)
	}

	t.Scope = scope.String
	t.UserID = userID.String

	// Check expiry
	if time.Now().Unix() > t.ExpiresAt {
		return nil, fmt.Errorf("access token expired: %s", tokenValue)
	}

	return &t, nil
}

// RevokeAccessToken deletes a specific access token.
func (s *StorageLayer) RevokeAccessToken(ctx context.Context, tokenValue string) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM oauth_access_tokens WHERE token = ?", tokenValue)
	if err != nil {
		return fmt.Errorf("revoke access token: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("access token not found: %s", tokenValue)
	}
	return nil
}

// RevokeAccessTokensByClient deletes all access tokens for a given client.
func (s *StorageLayer) RevokeAccessTokensByClient(ctx context.Context, clientID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM oauth_access_tokens WHERE client_id = ?", clientID)
	if err != nil {
		return fmt.Errorf("revoke access tokens by client: %w", err)
	}
	return nil
}

// CleanupExpiredAccessTokens removes all access tokens past their expires_at.
func (s *StorageLayer) CleanupExpiredAccessTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM oauth_access_tokens WHERE expires_at < ?", time.Now().Unix())
	if err != nil {
		return fmt.Errorf("cleanup expired access tokens: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Refresh token store
// ---------------------------------------------------------------------------

// CreateRefreshToken inserts a new refresh token.
func (s *StorageLayer) CreateRefreshToken(ctx context.Context, token *OAuthRefreshToken) error {
	if token.CreatedAt == 0 {
		token.CreatedAt = time.Now().Unix()
	}
	if token.ExpiresAt == 0 {
		token.ExpiresAt = time.Now().Add(RefreshTokenTTL).Unix()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO oauth_refresh_tokens
			(token, client_id, scope, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		token.Token, token.ClientID, token.Scope, token.UserID,
		token.ExpiresAt, token.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// ConsumeRefreshToken retrieves and deletes a refresh token (single-use rotation).
// Returns an error if the token is expired or not found.
func (s *StorageLayer) ConsumeRefreshToken(ctx context.Context, tokenValue string) (*OAuthRefreshToken, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var t OAuthRefreshToken
	var scope, userID sql.NullString

	err = tx.QueryRowContext(ctx, `
		SELECT token, client_id, scope, user_id, expires_at, created_at
		FROM oauth_refresh_tokens WHERE token = ?`, tokenValue,
	).Scan(&t.Token, &t.ClientID, &scope, &userID, &t.ExpiresAt, &t.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("refresh token not found: %s", tokenValue)
	}
	if err != nil {
		return nil, fmt.Errorf("query refresh token: %w", err)
	}

	t.Scope = scope.String
	t.UserID = userID.String

	// Check expiry
	if time.Now().Unix() > t.ExpiresAt {
		_, _ = tx.ExecContext(ctx, "DELETE FROM oauth_refresh_tokens WHERE token = ?", tokenValue)
		_ = tx.Commit()
		return nil, fmt.Errorf("refresh token expired: %s", tokenValue)
	}

	// Delete (single-use rotation)
	_, err = tx.ExecContext(ctx, "DELETE FROM oauth_refresh_tokens WHERE token = ?", tokenValue)
	if err != nil {
		return nil, fmt.Errorf("delete refresh token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &t, nil
}

// RevokeRefreshTokensByClient deletes all refresh tokens for a given client.
func (s *StorageLayer) RevokeRefreshTokensByClient(ctx context.Context, clientID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM oauth_refresh_tokens WHERE client_id = ?", clientID)
	if err != nil {
		return fmt.Errorf("revoke refresh tokens by client: %w", err)
	}
	return nil
}

// CleanupExpiredRefreshTokens removes all refresh tokens past their expires_at.
func (s *StorageLayer) CleanupExpiredRefreshTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM oauth_refresh_tokens WHERE expires_at < ?", time.Now().Unix())
	if err != nil {
		return fmt.Errorf("cleanup expired refresh tokens: %w", err)
	}
	return nil
}
