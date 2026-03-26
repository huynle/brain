package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// OAuth tables exist in schema
// ---------------------------------------------------------------------------

func TestSchemaCreation_OAuthTablesExist(t *testing.T) {
	s := newTestStorage(t)

	tables := []string{
		"oauth_clients",
		"oauth_auth_codes",
		"oauth_access_tokens",
		"oauth_refresh_tokens",
	}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
			).Scan(&name)
			if err != nil {
				t.Fatalf("table %q not found: %v", table, err)
			}
		})
	}
}

func TestSchemaCreation_OAuthIndexesExist(t *testing.T) {
	s := newTestStorage(t)

	indexes := []string{
		"idx_oauth_auth_codes_client",
		"idx_oauth_auth_codes_expires",
		"idx_oauth_access_tokens_client",
		"idx_oauth_access_tokens_expires",
		"idx_oauth_refresh_tokens_client",
		"idx_oauth_refresh_tokens_expires",
	}
	for _, idx := range indexes {
		t.Run(idx, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
			).Scan(&name)
			if err != nil {
				t.Fatalf("index %q not found: %v", idx, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Client store
// ---------------------------------------------------------------------------

func TestCreateOAuthClient_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	client := &OAuthClient{
		ClientSecret:            "secret123",
		RedirectURIs:            []string{"http://localhost:8080/callback"},
		ClientName:              "Test App",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_post",
	}

	err := s.CreateOAuthClient(ctx, client)
	if err != nil {
		t.Fatalf("CreateOAuthClient failed: %v", err)
	}

	// Client ID should be generated with brain_ prefix + 32 hex chars
	if !strings.HasPrefix(client.ClientID, "brain_") {
		t.Errorf("client_id = %q, want prefix 'brain_'", client.ClientID)
	}
	if len(client.ClientID) != 6+32 { // "brain_" + 32 hex
		t.Errorf("client_id length = %d, want %d", len(client.ClientID), 6+32)
	}

	// CreatedAt should be set
	if client.CreatedAt == 0 {
		t.Error("created_at should be set")
	}
}

func TestCreateOAuthClient_CustomClientID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	client := &OAuthClient{
		ClientID:      "custom-id",
		ClientSecret:  "secret",
		RedirectURIs:  []string{"http://localhost/cb"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	}

	err := s.CreateOAuthClient(ctx, client)
	if err != nil {
		t.Fatalf("CreateOAuthClient failed: %v", err)
	}
	if client.ClientID != "custom-id" {
		t.Errorf("client_id = %q, want %q", client.ClientID, "custom-id")
	}
}

func TestCreateOAuthClient_DuplicateID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	client := &OAuthClient{
		ClientID:      "dup-id",
		ClientSecret:  "secret",
		RedirectURIs:  []string{"http://localhost/cb"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	}

	err := s.CreateOAuthClient(ctx, client)
	if err != nil {
		t.Fatalf("first CreateOAuthClient failed: %v", err)
	}

	err = s.CreateOAuthClient(ctx, client)
	if err == nil {
		t.Fatal("expected error for duplicate client_id, got nil")
	}
}

func TestGetOAuthClient_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	client := &OAuthClient{
		ClientID:                "get-test",
		ClientSecret:            "secret",
		RedirectURIs:            []string{"http://localhost/cb", "http://localhost/cb2"},
		ClientName:              "My App",
		ClientURI:               "http://example.com",
		LogoURI:                 "http://example.com/logo.png",
		Scope:                   "read write",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_basic",
	}

	err := s.CreateOAuthClient(ctx, client)
	if err != nil {
		t.Fatalf("CreateOAuthClient failed: %v", err)
	}

	got, err := s.GetOAuthClient(ctx, "get-test")
	if err != nil {
		t.Fatalf("GetOAuthClient failed: %v", err)
	}

	if got.ClientID != "get-test" {
		t.Errorf("ClientID = %q, want %q", got.ClientID, "get-test")
	}
	if got.ClientSecret != "secret" {
		t.Errorf("ClientSecret = %q, want %q", got.ClientSecret, "secret")
	}
	if len(got.RedirectURIs) != 2 {
		t.Errorf("RedirectURIs count = %d, want 2", len(got.RedirectURIs))
	}
	if got.ClientName != "My App" {
		t.Errorf("ClientName = %q, want %q", got.ClientName, "My App")
	}
	if got.ClientURI != "http://example.com" {
		t.Errorf("ClientURI = %q, want %q", got.ClientURI, "http://example.com")
	}
	if got.Scope != "read write" {
		t.Errorf("Scope = %q, want %q", got.Scope, "read write")
	}
	if len(got.GrantTypes) != 2 {
		t.Errorf("GrantTypes count = %d, want 2", len(got.GrantTypes))
	}
	if got.TokenEndpointAuthMethod != "client_secret_basic" {
		t.Errorf("TokenEndpointAuthMethod = %q, want %q", got.TokenEndpointAuthMethod, "client_secret_basic")
	}
}

func TestGetOAuthClient_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	got, err := s.GetOAuthClient(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent client, got nil")
	}
	if got != nil {
		t.Errorf("expected nil client, got %v", got)
	}
}

func TestListOAuthClients_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		client := &OAuthClient{
			ClientSecret:  "secret",
			RedirectURIs:  []string{"http://localhost/cb"},
			GrantTypes:    []string{"authorization_code"},
			ResponseTypes: []string{"code"},
		}
		if err := s.CreateOAuthClient(ctx, client); err != nil {
			t.Fatalf("CreateOAuthClient %d failed: %v", i, err)
		}
	}

	clients, err := s.ListOAuthClients(ctx)
	if err != nil {
		t.Fatalf("ListOAuthClients failed: %v", err)
	}
	if len(clients) != 3 {
		t.Errorf("got %d clients, want 3", len(clients))
	}
}

func TestListOAuthClients_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	clients, err := s.ListOAuthClients(ctx)
	if err != nil {
		t.Fatalf("ListOAuthClients failed: %v", err)
	}
	if clients == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(clients) != 0 {
		t.Errorf("got %d clients, want 0", len(clients))
	}
}

// ---------------------------------------------------------------------------
// Auth code store
// ---------------------------------------------------------------------------

func createTestClient(t *testing.T, s *StorageLayer) *OAuthClient {
	t.Helper()
	ctx := context.Background()
	client := &OAuthClient{
		ClientSecret:  "secret",
		RedirectURIs:  []string{"http://localhost/cb"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	}
	if err := s.CreateOAuthClient(ctx, client); err != nil {
		t.Fatalf("createTestClient failed: %v", err)
	}
	return client
}

func TestCreateAuthCode_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	code := &OAuthAuthCode{
		Code:          "auth-code-123",
		ClientID:      client.ClientID,
		RedirectURI:   "http://localhost/cb",
		Scope:         "read",
		CodeChallenge: "challenge-value",
	}

	err := s.CreateAuthCode(ctx, code)
	if err != nil {
		t.Fatalf("CreateAuthCode failed: %v", err)
	}

	if code.CreatedAt == 0 {
		t.Error("created_at should be set")
	}
	if code.ExpiresAt == 0 {
		t.Error("expires_at should be set")
	}
	if code.CodeChallengeMethod != "S256" {
		t.Errorf("code_challenge_method = %q, want %q", code.CodeChallengeMethod, "S256")
	}
}

func TestConsumeAuthCode_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	code := &OAuthAuthCode{
		Code:          "consume-code-123",
		ClientID:      client.ClientID,
		RedirectURI:   "http://localhost/cb",
		Scope:         "read",
		CodeChallenge: "challenge",
		ExpiresAt:     time.Now().Add(10 * time.Minute).Unix(),
	}
	if err := s.CreateAuthCode(ctx, code); err != nil {
		t.Fatalf("CreateAuthCode failed: %v", err)
	}

	// Consume should succeed
	got, err := s.ConsumeAuthCode(ctx, "consume-code-123")
	if err != nil {
		t.Fatalf("ConsumeAuthCode failed: %v", err)
	}
	if got.Code != "consume-code-123" {
		t.Errorf("Code = %q, want %q", got.Code, "consume-code-123")
	}
	if got.ClientID != client.ClientID {
		t.Errorf("ClientID = %q, want %q", got.ClientID, client.ClientID)
	}
	if got.Scope != "read" {
		t.Errorf("Scope = %q, want %q", got.Scope, "read")
	}

	// Second consume should fail (single-use)
	_, err = s.ConsumeAuthCode(ctx, "consume-code-123")
	if err == nil {
		t.Fatal("expected error for already consumed code, got nil")
	}
}

func TestConsumeAuthCode_Expired(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	code := &OAuthAuthCode{
		Code:          "expired-code",
		ClientID:      client.ClientID,
		RedirectURI:   "http://localhost/cb",
		CodeChallenge: "challenge",
		ExpiresAt:     time.Now().Add(-1 * time.Minute).Unix(), // Already expired
		CreatedAt:     time.Now().Add(-11 * time.Minute).Unix(),
	}
	if err := s.CreateAuthCode(ctx, code); err != nil {
		t.Fatalf("CreateAuthCode failed: %v", err)
	}

	_, err := s.ConsumeAuthCode(ctx, "expired-code")
	if err == nil {
		t.Fatal("expected error for expired code, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %q, want to contain 'expired'", err.Error())
	}
}

func TestConsumeAuthCode_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.ConsumeAuthCode(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent code, got nil")
	}
}

func TestCleanupExpiredCodes(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	// Create an expired code
	expired := &OAuthAuthCode{
		Code:          "expired-cleanup",
		ClientID:      client.ClientID,
		RedirectURI:   "http://localhost/cb",
		CodeChallenge: "challenge",
		ExpiresAt:     time.Now().Add(-1 * time.Minute).Unix(),
		CreatedAt:     time.Now().Add(-11 * time.Minute).Unix(),
	}
	if err := s.CreateAuthCode(ctx, expired); err != nil {
		t.Fatalf("CreateAuthCode (expired) failed: %v", err)
	}

	// Create a valid code
	valid := &OAuthAuthCode{
		Code:          "valid-cleanup",
		ClientID:      client.ClientID,
		RedirectURI:   "http://localhost/cb",
		CodeChallenge: "challenge",
		ExpiresAt:     time.Now().Add(10 * time.Minute).Unix(),
	}
	if err := s.CreateAuthCode(ctx, valid); err != nil {
		t.Fatalf("CreateAuthCode (valid) failed: %v", err)
	}

	// Cleanup
	err := s.CleanupExpiredCodes(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredCodes failed: %v", err)
	}

	// Expired should be gone
	var count int
	err = s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM oauth_auth_codes WHERE code = ?", "expired-cleanup",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query expired code failed: %v", err)
	}
	if count != 0 {
		t.Error("expired code should be cleaned up")
	}

	// Valid should still exist
	err = s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM oauth_auth_codes WHERE code = ?", "valid-cleanup",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query valid code failed: %v", err)
	}
	if count != 1 {
		t.Error("valid code should still exist")
	}
}

// ---------------------------------------------------------------------------
// Access token store
// ---------------------------------------------------------------------------

func TestCreateAccessToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	token := &OAuthAccessToken{
		Token:    "access-token-123",
		ClientID: client.ClientID,
		Scope:    "read write",
		UserID:   "user1",
	}

	err := s.CreateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("CreateAccessToken failed: %v", err)
	}

	if token.CreatedAt == 0 {
		t.Error("created_at should be set")
	}
	if token.ExpiresAt == 0 {
		t.Error("expires_at should be set")
	}
}

func TestGetAccessToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	token := &OAuthAccessToken{
		Token:     "get-access-123",
		ClientID:  client.ClientID,
		Scope:     "read",
		UserID:    "user1",
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}
	if err := s.CreateAccessToken(ctx, token); err != nil {
		t.Fatalf("CreateAccessToken failed: %v", err)
	}

	got, err := s.GetAccessToken(ctx, "get-access-123")
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	if got.Token != "get-access-123" {
		t.Errorf("Token = %q, want %q", got.Token, "get-access-123")
	}
	if got.ClientID != client.ClientID {
		t.Errorf("ClientID = %q, want %q", got.ClientID, client.ClientID)
	}
	if got.Scope != "read" {
		t.Errorf("Scope = %q, want %q", got.Scope, "read")
	}
	if got.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "user1")
	}
}

func TestGetAccessToken_Expired(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	token := &OAuthAccessToken{
		Token:     "expired-access",
		ClientID:  client.ClientID,
		ExpiresAt: time.Now().Add(-1 * time.Minute).Unix(),
		CreatedAt: time.Now().Add(-2 * time.Hour).Unix(),
	}
	if err := s.CreateAccessToken(ctx, token); err != nil {
		t.Fatalf("CreateAccessToken failed: %v", err)
	}

	_, err := s.GetAccessToken(ctx, "expired-access")
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %q, want to contain 'expired'", err.Error())
	}
}

func TestGetAccessToken_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.GetAccessToken(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
}

func TestRevokeAccessToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	token := &OAuthAccessToken{
		Token:     "revoke-access",
		ClientID:  client.ClientID,
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}
	if err := s.CreateAccessToken(ctx, token); err != nil {
		t.Fatalf("CreateAccessToken failed: %v", err)
	}

	err := s.RevokeAccessToken(ctx, "revoke-access")
	if err != nil {
		t.Fatalf("RevokeAccessToken failed: %v", err)
	}

	// Token should be gone
	_, err = s.GetAccessToken(ctx, "revoke-access")
	if err == nil {
		t.Error("expected error after revocation, got nil")
	}
}

func TestRevokeAccessToken_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.RevokeAccessToken(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
}

func TestRevokeAccessTokensByClient_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	// Create multiple tokens for the same client
	for i := 0; i < 3; i++ {
		token := &OAuthAccessToken{
			Token:     "client-token-" + string(rune('a'+i)),
			ClientID:  client.ClientID,
			ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
		}
		if err := s.CreateAccessToken(ctx, token); err != nil {
			t.Fatalf("CreateAccessToken %d failed: %v", i, err)
		}
	}

	err := s.RevokeAccessTokensByClient(ctx, client.ClientID)
	if err != nil {
		t.Fatalf("RevokeAccessTokensByClient failed: %v", err)
	}

	// All tokens for this client should be gone
	var count int
	err = s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM oauth_access_tokens WHERE client_id = ?", client.ClientID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tokens after revoke by client, got %d", count)
	}
}

func TestCleanupExpiredAccessTokens(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	// Expired
	expired := &OAuthAccessToken{
		Token:     "expired-cleanup-access",
		ClientID:  client.ClientID,
		ExpiresAt: time.Now().Add(-1 * time.Minute).Unix(),
		CreatedAt: time.Now().Add(-2 * time.Hour).Unix(),
	}
	if err := s.CreateAccessToken(ctx, expired); err != nil {
		t.Fatalf("CreateAccessToken (expired) failed: %v", err)
	}

	// Valid
	valid := &OAuthAccessToken{
		Token:     "valid-cleanup-access",
		ClientID:  client.ClientID,
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}
	if err := s.CreateAccessToken(ctx, valid); err != nil {
		t.Fatalf("CreateAccessToken (valid) failed: %v", err)
	}

	err := s.CleanupExpiredAccessTokens(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredAccessTokens failed: %v", err)
	}

	// Expired should be gone
	var count int
	s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM oauth_access_tokens WHERE token = ?", "expired-cleanup-access",
	).Scan(&count)
	if count != 0 {
		t.Error("expired access token should be cleaned up")
	}

	// Valid should remain
	s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM oauth_access_tokens WHERE token = ?", "valid-cleanup-access",
	).Scan(&count)
	if count != 1 {
		t.Error("valid access token should still exist")
	}
}

// ---------------------------------------------------------------------------
// Refresh token store
// ---------------------------------------------------------------------------

func TestCreateRefreshToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	token := &OAuthRefreshToken{
		Token:    "refresh-token-123",
		ClientID: client.ClientID,
		Scope:    "read",
		UserID:   "user1",
	}

	err := s.CreateRefreshToken(ctx, token)
	if err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	if token.CreatedAt == 0 {
		t.Error("created_at should be set")
	}
	if token.ExpiresAt == 0 {
		t.Error("expires_at should be set")
	}
}

func TestConsumeRefreshToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	token := &OAuthRefreshToken{
		Token:     "consume-refresh-123",
		ClientID:  client.ClientID,
		Scope:     "read",
		UserID:    "user1",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	if err := s.CreateRefreshToken(ctx, token); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	got, err := s.ConsumeRefreshToken(ctx, "consume-refresh-123")
	if err != nil {
		t.Fatalf("ConsumeRefreshToken failed: %v", err)
	}
	if got.Token != "consume-refresh-123" {
		t.Errorf("Token = %q, want %q", got.Token, "consume-refresh-123")
	}
	if got.Scope != "read" {
		t.Errorf("Scope = %q, want %q", got.Scope, "read")
	}
	if got.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "user1")
	}

	// Second consume should fail (single-use rotation)
	_, err = s.ConsumeRefreshToken(ctx, "consume-refresh-123")
	if err == nil {
		t.Fatal("expected error for already consumed refresh token, got nil")
	}
}

func TestConsumeRefreshToken_Expired(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	token := &OAuthRefreshToken{
		Token:     "expired-refresh",
		ClientID:  client.ClientID,
		ExpiresAt: time.Now().Add(-1 * time.Minute).Unix(),
		CreatedAt: time.Now().Add(-8 * 24 * time.Hour).Unix(),
	}
	if err := s.CreateRefreshToken(ctx, token); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	_, err := s.ConsumeRefreshToken(ctx, "expired-refresh")
	if err == nil {
		t.Fatal("expected error for expired refresh token, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %q, want to contain 'expired'", err.Error())
	}
}

func TestConsumeRefreshToken_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.ConsumeRefreshToken(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent refresh token, got nil")
	}
}

func TestRevokeRefreshTokensByClient_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	for i := 0; i < 3; i++ {
		token := &OAuthRefreshToken{
			Token:     "client-refresh-" + string(rune('a'+i)),
			ClientID:  client.ClientID,
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
		}
		if err := s.CreateRefreshToken(ctx, token); err != nil {
			t.Fatalf("CreateRefreshToken %d failed: %v", i, err)
		}
	}

	err := s.RevokeRefreshTokensByClient(ctx, client.ClientID)
	if err != nil {
		t.Fatalf("RevokeRefreshTokensByClient failed: %v", err)
	}

	var count int
	s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM oauth_refresh_tokens WHERE client_id = ?", client.ClientID,
	).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 refresh tokens after revoke by client, got %d", count)
	}
}

func TestCleanupExpiredRefreshTokens(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	client := createTestClient(t, s)

	// Expired
	expired := &OAuthRefreshToken{
		Token:     "expired-cleanup-refresh",
		ClientID:  client.ClientID,
		ExpiresAt: time.Now().Add(-1 * time.Minute).Unix(),
		CreatedAt: time.Now().Add(-8 * 24 * time.Hour).Unix(),
	}
	if err := s.CreateRefreshToken(ctx, expired); err != nil {
		t.Fatalf("CreateRefreshToken (expired) failed: %v", err)
	}

	// Valid
	valid := &OAuthRefreshToken{
		Token:     "valid-cleanup-refresh",
		ClientID:  client.ClientID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	if err := s.CreateRefreshToken(ctx, valid); err != nil {
		t.Fatalf("CreateRefreshToken (valid) failed: %v", err)
	}

	err := s.CleanupExpiredRefreshTokens(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredRefreshTokens failed: %v", err)
	}

	var count int
	s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM oauth_refresh_tokens WHERE token = ?", "expired-cleanup-refresh",
	).Scan(&count)
	if count != 0 {
		t.Error("expired refresh token should be cleaned up")
	}

	s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM oauth_refresh_tokens WHERE token = ?", "valid-cleanup-refresh",
	).Scan(&count)
	if count != 1 {
		t.Error("valid refresh token should still exist")
	}
}

// ---------------------------------------------------------------------------
// Foreign key enforcement for OAuth tables
// ---------------------------------------------------------------------------

func TestOAuthForeignKeys_AuthCodeRequiresClient(t *testing.T) {
	s := newTestStorage(t)

	_, err := s.DB().Exec(`
		INSERT INTO oauth_auth_codes 
			(code, client_id, redirect_uri, code_challenge, code_challenge_method, expires_at, created_at)
		VALUES ('test', 'nonexistent', 'http://localhost', 'challenge', 'S256', 9999999999, 1000000000)
	`)
	if err == nil {
		t.Fatal("expected foreign key error for invalid client_id, got nil")
	}
}

func TestOAuthForeignKeys_AccessTokenRequiresClient(t *testing.T) {
	s := newTestStorage(t)

	_, err := s.DB().Exec(`
		INSERT INTO oauth_access_tokens 
			(token, client_id, scope, expires_at, created_at)
		VALUES ('test', 'nonexistent', 'read', 9999999999, 1000000000)
	`)
	if err == nil {
		t.Fatal("expected foreign key error for invalid client_id, got nil")
	}
}

func TestOAuthForeignKeys_RefreshTokenRequiresClient(t *testing.T) {
	s := newTestStorage(t)

	_, err := s.DB().Exec(`
		INSERT INTO oauth_refresh_tokens 
			(token, client_id, scope, expires_at, created_at)
		VALUES ('test', 'nonexistent', 'read', 9999999999, 1000000000)
	`)
	if err == nil {
		t.Fatal("expected foreign key error for invalid client_id, got nil")
	}
}

// ---------------------------------------------------------------------------
// Schema version is updated to 3
// ---------------------------------------------------------------------------

func TestSchemaVersion_IsThree(t *testing.T) {
	s := newTestStorage(t)

	var ver int
	err := s.DB().QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&ver)
	if err != nil {
		t.Fatalf("query schema version failed: %v", err)
	}
	if ver != 3 {
		t.Errorf("schema version = %d, want 3", ver)
	}
}
