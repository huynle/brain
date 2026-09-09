package storage

import (
	"context"
	"errors"

	"github.com/huynle/brain-api/internal/tenant"
)

// SingleModeTokenStore is the temporary P3.5 compatibility exception for local
// installations. It is NOT an operator capability, has no registry/DB/control
// escape, and cannot be constructed for multi mode. HTTP authentication and
// scope checks remain at the router; bootstrap additionally enforces the P1
// peer/password/permanent-claim safeguards. Replace in P6/P7 BEFORE multi mode.
type SingleModeTokenStore struct {
	backing *StorageLayer
	mode    tenant.Mode
}

// SingleModeTokens is reserved for the audited composition owner, never a
// request-selected mode. Requiring explicit single rejects unset/unknown modes
// rather than silently converting them into this compatibility authorization.
func (s *StorageLayer) SingleModeTokens(mode tenant.Mode) (*SingleModeTokenStore, error) {
	if mode != tenant.ModeSingle || s == nil {
		return nil, errors.New("token compatibility requires explicit single mode and storage owner")
	}
	return &SingleModeTokenStore{backing: s, mode: mode}, nil
}

func (s *SingleModeTokenStore) allowed() error {
	if s == nil || s.backing == nil || s.mode != tenant.ModeSingle {
		return errors.New("token compatibility unavailable outside single mode")
	}
	return nil
}

func (s *SingleModeTokenStore) GenerateToken() (string, error) {
	if err := s.allowed(); err != nil {
		return "", err
	}
	return s.backing.GenerateToken()
}
func (s *SingleModeTokenStore) CreateToken(ctx context.Context, name, token, scope string) error {
	if err := s.allowed(); err != nil {
		return err
	}
	return s.backing.CreateToken(ctx, name, token, scope)
}
func (s *SingleModeTokenStore) ListTokens(ctx context.Context, revoked ...bool) ([]Token, error) {
	if err := s.allowed(); err != nil {
		return nil, err
	}
	return s.backing.ListTokens(ctx, revoked...)
}
func (s *SingleModeTokenStore) GetTokenByName(ctx context.Context, name string) (*Token, error) {
	if err := s.allowed(); err != nil {
		return nil, err
	}
	return s.backing.GetTokenByName(ctx, name)
}
func (s *SingleModeTokenStore) RevokeToken(ctx context.Context, name string) error {
	if err := s.allowed(); err != nil {
		return err
	}
	return s.backing.RevokeToken(ctx, name)
}
func (s *SingleModeTokenStore) BootstrapToken(ctx context.Context, name, token string, passwordConfigured bool) error {
	if err := s.allowed(); err != nil {
		return err
	}
	return s.backing.BootstrapToken(ctx, name, token, passwordConfigured)
}
