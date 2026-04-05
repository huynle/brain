package storage

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// GenerateToken
// ---------------------------------------------------------------------------

func TestGenerateToken_Success(t *testing.T) {
	s := newTestStorage(t)

	token, err := s.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Token should be non-empty
	if token == "" {
		t.Fatal("GenerateToken returned empty token")
	}

	// Token should be base64 URL-safe (43 chars for 32 bytes)
	if len(token) != 43 {
		t.Errorf("token length = %d, want 43", len(token))
	}
}

func TestGenerateToken_Unique(t *testing.T) {
	s := newTestStorage(t)

	token1, err := s.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken (1) failed: %v", err)
	}

	token2, err := s.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken (2) failed: %v", err)
	}

	// Tokens should be different
	if token1 == token2 {
		t.Errorf("generated same token twice: %q", token1)
	}
}

// ---------------------------------------------------------------------------
// CreateToken
// ---------------------------------------------------------------------------

func TestCreateToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, err := s.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	err = s.CreateToken(ctx, "test-token", token, "")
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	// Verify token was stored with default scope
	var storedToken string
	var createdAt string
	var scope string
	err = s.DB().QueryRowContext(ctx,
		"SELECT token, scope, created_at FROM api_tokens WHERE name = ?", "test-token",
	).Scan(&storedToken, &scope, &createdAt)
	if err != nil {
		t.Fatalf("query token failed: %v", err)
	}

	if storedToken != token {
		t.Errorf("stored token = %q, want %q", storedToken, token)
	}
	if scope != "admin:*" {
		t.Errorf("scope = %q, want %q (default)", scope, "admin:*")
	}
	if createdAt == "" {
		t.Error("created_at should not be empty")
	}
}

func TestCreateToken_DuplicateName(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token1, _ := s.GenerateToken()
	token2, _ := s.GenerateToken()

	// Create first token
	err := s.CreateToken(ctx, "duplicate", token1, "")
	if err != nil {
		t.Fatalf("CreateToken (1) failed: %v", err)
	}

	// Creating with same name should fail
	err = s.CreateToken(ctx, "duplicate", token2, "")
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

func TestCreateToken_DuplicateToken(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, _ := s.GenerateToken()

	// Create first token
	err := s.CreateToken(ctx, "name1", token, "")
	if err != nil {
		t.Fatalf("CreateToken (1) failed: %v", err)
	}

	// Creating with same token should fail
	err = s.CreateToken(ctx, "name2", token, "")
	if err == nil {
		t.Fatal("expected error for duplicate token, got nil")
	}
}

// ---------------------------------------------------------------------------
// ValidateToken
// ---------------------------------------------------------------------------

func TestValidateToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokenValue, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "validate-me", tokenValue, "")

	// Validate should return the token
	token, err := s.ValidateToken(ctx, tokenValue)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if token == nil {
		t.Fatal("expected token, got nil")
	}
	if token.Name != "validate-me" {
		t.Errorf("name = %q, want %q", token.Name, "validate-me")
	}
	if token.Token != tokenValue {
		t.Errorf("token value should be the full token")
	}
}

func TestValidateToken_RevokedToken(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokenValue, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "revoke-validate", tokenValue, "")

	// Revoke the token
	err := s.RevokeToken(ctx, "revoke-validate")
	if err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// ValidateToken should fail for revoked token
	token, err := s.ValidateToken(ctx, tokenValue)
	if err == nil {
		t.Fatal("expected error for revoked token, got nil")
	}
	if token != nil {
		t.Errorf("expected nil token for revoked, got %v", token)
	}
}

func TestValidateToken_UnknownToken(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// ValidateToken should fail for unknown token
	token, err := s.ValidateToken(ctx, "nonexistent-token-value")
	if err == nil {
		t.Fatal("expected error for unknown token, got nil")
	}
	if token != nil {
		t.Errorf("expected nil token for unknown, got %v", token)
	}
}

func TestValidateToken_UpdatesLastUsed(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokenValue, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "last-used-test", tokenValue, "")

	// Validate the token
	_, err := s.ValidateToken(ctx, tokenValue)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	// Give the async goroutine time to complete
	time.Sleep(100 * time.Millisecond)

	// Check that last_used was updated
	var lastUsed string
	err = s.DB().QueryRowContext(ctx,
		"SELECT COALESCE(last_used, '') FROM api_tokens WHERE name = ?", "last-used-test",
	).Scan(&lastUsed)
	if err != nil {
		t.Fatalf("query last_used failed: %v", err)
	}
	if lastUsed == "" {
		t.Error("last_used should be set after ValidateToken")
	}
}

// ---------------------------------------------------------------------------
// ListTokens
// ---------------------------------------------------------------------------

func TestListTokens_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create some tokens
	token1, _ := s.GenerateToken()
	token2, _ := s.GenerateToken()
	token3, _ := s.GenerateToken()

	_ = s.CreateToken(ctx, "token1", token1, "")
	_ = s.CreateToken(ctx, "token2", token2, "")
	_ = s.CreateToken(ctx, "token3", token3, "")

	tokens, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens failed: %v", err)
	}

	if len(tokens) != 3 {
		t.Fatalf("got %d tokens, want 3", len(tokens))
	}

	// Check first token structure
	if tokens[0].Name == "" {
		t.Error("token Name should not be empty")
	}
	if tokens[0].Token == "" {
		t.Error("token Token should not be empty")
	}
	if tokens[0].CreatedAt == "" {
		t.Error("token CreatedAt should not be empty")
	}
}

func TestListTokens_ReturnsPrefix(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokenValue, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "prefix-test", tokenValue, "")

	tokens, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("got %d tokens, want 1", len(tokens))
	}

	// Token should be prefix only (8 chars)
	if len(tokens[0].Token) != 8 {
		t.Errorf("listed token length = %d, want 8 (prefix only)", len(tokens[0].Token))
	}
	if tokens[0].Token != tokenValue[:8] {
		t.Errorf("listed token = %q, want prefix %q", tokens[0].Token, tokenValue[:8])
	}
}

func TestListTokens_ExcludesRevokedByDefault(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token1, _ := s.GenerateToken()
	token2, _ := s.GenerateToken()

	_ = s.CreateToken(ctx, "active-token", token1, "")
	_ = s.CreateToken(ctx, "revoked-token", token2, "")
	_ = s.RevokeToken(ctx, "revoked-token")

	// Default: exclude revoked
	tokens, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("got %d tokens, want 1 (revoked should be excluded)", len(tokens))
	}
	if tokens[0].Name != "active-token" {
		t.Errorf("name = %q, want %q", tokens[0].Name, "active-token")
	}
}

func TestListTokens_IncludeRevoked(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token1, _ := s.GenerateToken()
	token2, _ := s.GenerateToken()

	_ = s.CreateToken(ctx, "active-token", token1, "")
	_ = s.CreateToken(ctx, "revoked-token", token2, "")
	_ = s.RevokeToken(ctx, "revoked-token")

	// Include revoked
	tokens, err := s.ListTokens(ctx, true)
	if err != nil {
		t.Fatalf("ListTokens(includeRevoked) failed: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2 (including revoked)", len(tokens))
	}
}

func TestListTokens_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokens, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens failed: %v", err)
	}

	if tokens == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(tokens) != 0 {
		t.Errorf("got %d tokens, want 0", len(tokens))
	}
}

// ---------------------------------------------------------------------------
// GetTokenByName
// ---------------------------------------------------------------------------

func TestGetTokenByName_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "test-token", token, "")

	retrieved, err := s.GetTokenByName(ctx, "test-token")
	if err != nil {
		t.Fatalf("GetTokenByName failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("expected token, got nil")
	}
	if retrieved.Name != "test-token" {
		t.Errorf("name = %q, want %q", retrieved.Name, "test-token")
	}
	if retrieved.Token != token {
		t.Errorf("token = %q, want %q", retrieved.Token, token)
	}
}

func TestGetTokenByName_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	retrieved, err := s.GetTokenByName(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
	if retrieved != nil {
		t.Errorf("expected nil token, got %v", retrieved)
	}
}

// ---------------------------------------------------------------------------
// RevokeToken (soft-revocation)
// ---------------------------------------------------------------------------

func TestRevokeToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokenValue, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "revoke-me", tokenValue, "")

	err := s.RevokeToken(ctx, "revoke-me")
	if err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// Token should still exist in DB but have revoked_at set
	var revokedAt string
	err = s.DB().QueryRowContext(ctx,
		"SELECT COALESCE(revoked_at, '') FROM api_tokens WHERE name = ?", "revoke-me",
	).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("query revoked_at failed: %v", err)
	}
	if revokedAt == "" {
		t.Error("revoked_at should be set after RevokeToken")
	}

	// Token should be retrievable by name (it's still in DB)
	retrieved, err := s.GetTokenByName(ctx, "revoke-me")
	if err != nil {
		t.Fatalf("GetTokenByName after revoke should still work: %v", err)
	}
	if retrieved.RevokedAt == "" {
		t.Error("RevokedAt field should be set on retrieved token")
	}
}

func TestRevokeToken_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.RevokeToken(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
}

func TestRevokeToken_AlreadyRevoked(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokenValue, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "double-revoke", tokenValue, "")

	// Revoke once
	err := s.RevokeToken(ctx, "double-revoke")
	if err != nil {
		t.Fatalf("first RevokeToken failed: %v", err)
	}

	// Revoke again should fail (already revoked)
	err = s.RevokeToken(ctx, "double-revoke")
	if err == nil {
		t.Fatal("expected error for already revoked token, got nil")
	}
}

// ---------------------------------------------------------------------------
// DeleteTokenPermanent
// ---------------------------------------------------------------------------

func TestDeleteTokenPermanent_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "delete-me", token, "")

	err := s.DeleteTokenPermanent(ctx, "delete-me")
	if err != nil {
		t.Fatalf("DeleteTokenPermanent failed: %v", err)
	}

	// Verify token is gone
	_, err = s.GetTokenByName(ctx, "delete-me")
	if err == nil {
		t.Error("token should be permanently deleted")
	}
}

func TestDeleteTokenPermanent_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.DeleteTokenPermanent(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
}

// ---------------------------------------------------------------------------
// UpdateTokenLastUsed
// ---------------------------------------------------------------------------

func TestUpdateTokenLastUsed_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "use-me", token, "")

	// Update last_used
	err := s.UpdateTokenLastUsed(ctx, "use-me")
	if err != nil {
		t.Fatalf("UpdateTokenLastUsed failed: %v", err)
	}

	// Verify last_used is set
	var lastUsed string
	err = s.DB().QueryRowContext(ctx,
		"SELECT last_used FROM api_tokens WHERE name = ?", "use-me",
	).Scan(&lastUsed)
	if err != nil {
		t.Fatalf("query last_used failed: %v", err)
	}
	if lastUsed == "" {
		t.Error("last_used should not be empty after update")
	}
}

// ---------------------------------------------------------------------------
// CountActiveTokens
// ---------------------------------------------------------------------------

func TestCountActiveTokens_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	count, err := s.CountActiveTokens(ctx)
	if err != nil {
		t.Fatalf("CountActiveTokens failed: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestCountActiveTokens_WithTokens(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token1, _ := s.GenerateToken()
	token2, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "token1", token1, "")
	_ = s.CreateToken(ctx, "token2", token2, "")

	count, err := s.CountActiveTokens(ctx)
	if err != nil {
		t.Fatalf("CountActiveTokens failed: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestCountActiveTokens_ExcludesRevoked(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token1, _ := s.GenerateToken()
	token2, _ := s.GenerateToken()
	token3, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "active1", token1, "")
	_ = s.CreateToken(ctx, "active2", token2, "")
	_ = s.CreateToken(ctx, "revoked1", token3, "")
	_ = s.RevokeToken(ctx, "revoked1")

	count, err := s.CountActiveTokens(ctx)
	if err != nil {
		t.Fatalf("CountActiveTokens failed: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (revoked should be excluded)", count)
	}
}

func TestUpdateTokenLastUsed_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.UpdateTokenLastUsed(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
}

// ---------------------------------------------------------------------------
// Token Scopes
// ---------------------------------------------------------------------------

func TestCreateToken_WithScope(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, _ := s.GenerateToken()
	err := s.CreateToken(ctx, "runner-token", token, "runner:*")
	if err != nil {
		t.Fatalf("CreateToken with scope failed: %v", err)
	}

	// Verify scope stored correctly
	var scope string
	err = s.DB().QueryRowContext(ctx,
		"SELECT scope FROM api_tokens WHERE name = ?", "runner-token",
	).Scan(&scope)
	if err != nil {
		t.Fatalf("query scope failed: %v", err)
	}
	if scope != "runner:*" {
		t.Errorf("scope = %q, want %q", scope, "runner:*")
	}
}

func TestCreateToken_DefaultScope(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, _ := s.GenerateToken()
	err := s.CreateToken(ctx, "default-scope", token, "")
	if err != nil {
		t.Fatalf("CreateToken with empty scope failed: %v", err)
	}

	// Verify default scope is admin:*
	var scope string
	err = s.DB().QueryRowContext(ctx,
		"SELECT scope FROM api_tokens WHERE name = ?", "default-scope",
	).Scan(&scope)
	if err != nil {
		t.Fatalf("query scope failed: %v", err)
	}
	if scope != "admin:*" {
		t.Errorf("scope = %q, want %q (default)", scope, "admin:*")
	}
}

func TestValidateToken_ReturnsScope(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokenValue, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "scoped-token", tokenValue, "runner:*")

	token, err := s.ValidateToken(ctx, tokenValue)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if token.Scope != "runner:*" {
		t.Errorf("scope = %q, want %q", token.Scope, "runner:*")
	}
}

func TestListTokens_IncludesScope(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token1, _ := s.GenerateToken()
	token2, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "admin-token", token1, "admin:*")
	_ = s.CreateToken(ctx, "runner-token", token2, "runner:*")

	tokens, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens failed: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}

	// Find each token and verify scope
	scopesByName := map[string]string{}
	for _, tok := range tokens {
		scopesByName[tok.Name] = tok.Scope
	}
	if scopesByName["admin-token"] != "admin:*" {
		t.Errorf("admin-token scope = %q, want %q", scopesByName["admin-token"], "admin:*")
	}
	if scopesByName["runner-token"] != "runner:*" {
		t.Errorf("runner-token scope = %q, want %q", scopesByName["runner-token"], "runner:*")
	}
}

func TestGetTokenByName_ReturnsScope(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	tokenValue, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "read-token", tokenValue, "read:*")

	token, err := s.GetTokenByName(ctx, "read-token")
	if err != nil {
		t.Fatalf("GetTokenByName failed: %v", err)
	}
	if token.Scope != "read:*" {
		t.Errorf("scope = %q, want %q", token.Scope, "read:*")
	}
}
