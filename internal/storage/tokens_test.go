package storage

import (
	"context"
	"testing"
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

	err = s.CreateToken(ctx, "test-token", token)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	// Verify token was stored
	var storedToken string
	var createdAt string
	err = s.DB().QueryRowContext(ctx,
		"SELECT token, created_at FROM api_tokens WHERE name = ?", "test-token",
	).Scan(&storedToken, &createdAt)
	if err != nil {
		t.Fatalf("query token failed: %v", err)
	}

	if storedToken != token {
		t.Errorf("stored token = %q, want %q", storedToken, token)
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
	err := s.CreateToken(ctx, "duplicate", token1)
	if err != nil {
		t.Fatalf("CreateToken (1) failed: %v", err)
	}

	// Creating with same name should fail
	err = s.CreateToken(ctx, "duplicate", token2)
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

func TestCreateToken_DuplicateToken(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, _ := s.GenerateToken()

	// Create first token
	err := s.CreateToken(ctx, "name1", token)
	if err != nil {
		t.Fatalf("CreateToken (1) failed: %v", err)
	}

	// Creating with same token should fail
	err = s.CreateToken(ctx, "name2", token)
	if err == nil {
		t.Fatal("expected error for duplicate token, got nil")
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

	_ = s.CreateToken(ctx, "token1", token1)
	_ = s.CreateToken(ctx, "token2", token2)
	_ = s.CreateToken(ctx, "token3", token3)

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
	_ = s.CreateToken(ctx, "test-token", token)

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
// DeleteToken
// ---------------------------------------------------------------------------

func TestDeleteToken_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token, _ := s.GenerateToken()
	_ = s.CreateToken(ctx, "delete-me", token)

	err := s.DeleteToken(ctx, "delete-me")
	if err != nil {
		t.Fatalf("DeleteToken failed: %v", err)
	}

	// Verify token is gone
	_, err = s.GetTokenByName(ctx, "delete-me")
	if err == nil {
		t.Error("token should be deleted")
	}
}

func TestDeleteToken_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.DeleteToken(ctx, "nonexistent")
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
	_ = s.CreateToken(ctx, "use-me", token)

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

func TestUpdateTokenLastUsed_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.UpdateTokenLastUsed(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
}
