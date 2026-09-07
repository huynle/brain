package storage

import (
	"context"
	"errors"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/tenantfs"
)

// ControlStore exposes explicitly reviewed identity operations, never workload
// queries or raw DB access. Its named backing must NOT become an embedding.
// System-wide operations are delegated only through capability-bound adapters.
type ControlStore struct{ backing *StorageLayer }

// Control wraps the existing pool without SQL, schema changes or registration.
// The StorageLayer owner remains responsible for initialization and Close.
func (s *StorageLayer) Control() (*ControlStore, error) {
	if s == nil {
		return nil, errors.New("nil storage layer")
	}
	return &ControlStore{backing: s}, nil
}

// ValidateToken authenticates possession of an API token; even admin:* does not
// mint a deployment-operator capability.
func (c *ControlStore) ValidateToken(ctx context.Context, token string) (*Token, error) {
	return c.backing.ValidateToken(ctx, token)
}

// GetAccessToken authenticates possession of a nonexpired OAuth access token.
func (c *ControlStore) GetAccessToken(ctx context.Context, token string) (*OAuthAccessToken, error) {
	return c.backing.GetAccessToken(ctx, token)
}

// TenantRegistry binds operator authority to tenantfs.Repository's existing
// narrow interface. Retain it only within trusted filesystem composition; never
// hand the repository or resolver to an ordinary request as provisioning power.
func (c *ControlStore) TenantRegistry(cap auth.DeploymentOperator) (tenantfs.Repository, error) {
	if !cap.Valid() {
		return nil, auth.ErrOperatorRequired
	}
	if c == nil || c.backing == nil {
		return nil, errors.New("nil control store")
	}
	return &tenantRegistry{backing: c.backing, operator: cap}, nil
}

type tenantRegistry struct {
	backing  *StorageLayer
	operator auth.DeploymentOperator
}

func (r *tenantRegistry) ListTenantRoots(ctx context.Context) ([]tenantfs.Mapping, error) {
	if r == nil || !r.operator.Valid() {
		return nil, auth.ErrOperatorRequired
	}
	return r.backing.ListTenantRoots(ctx)
}

func (r *tenantRegistry) RegisterTenantRoots(ctx context.Context, m tenantfs.Mapping, validate func([]tenantfs.Mapping) error) error {
	if r == nil || !r.operator.Valid() {
		return auth.ErrOperatorRequired
	}
	return r.backing.RegisterTenantRoots(ctx, m, validate)
}

// TokenAdmin binds deployment-operator authority to the existing token admin
// method signatures (including the offline permanent-delete operation). Do not
// install a privileged adapter in an ordinary admin-scope handler: that would
// delegate operator power to every request served by that handler.
func (c *ControlStore) TokenAdmin(cap auth.DeploymentOperator) (*TokenAdmin, error) {
	if !cap.Valid() {
		return nil, auth.ErrOperatorRequired
	}
	if c == nil || c.backing == nil {
		return nil, errors.New("nil control store")
	}
	return &TokenAdmin{backing: c.backing, operator: cap}, nil
}

// TokenAdmin is a capability-bound adapter, not a StorageLayer embedding.
// Its zero value rejects every operation before touching the backing store.
type TokenAdmin struct {
	backing  *StorageLayer
	operator auth.DeploymentOperator
}

func (a *TokenAdmin) authorized() error {
	if a == nil || !a.operator.Valid() {
		return auth.ErrOperatorRequired
	}
	return nil
}

func (a *TokenAdmin) GenerateToken() (string, error) {
	if err := a.authorized(); err != nil {
		return "", err
	}
	return a.backing.GenerateToken()
}

func (a *TokenAdmin) CreateToken(ctx context.Context, name, token, scope string) error {
	if err := a.authorized(); err != nil {
		return err
	}
	return a.backing.CreateToken(ctx, name, token, scope)
}

func (a *TokenAdmin) ListTokens(ctx context.Context, includeRevoked ...bool) ([]Token, error) {
	if err := a.authorized(); err != nil {
		return nil, err
	}
	return a.backing.ListTokens(ctx, includeRevoked...)
}

func (a *TokenAdmin) GetTokenByName(ctx context.Context, name string) (*Token, error) {
	if err := a.authorized(); err != nil {
		return nil, err
	}
	return a.backing.GetTokenByName(ctx, name)
}

func (a *TokenAdmin) RevokeToken(ctx context.Context, name string) error {
	if err := a.authorized(); err != nil {
		return err
	}
	return a.backing.RevokeToken(ctx, name)
}

func (a *TokenAdmin) DeleteTokenPermanent(ctx context.Context, name string) error {
	if err := a.authorized(); err != nil {
		return err
	}
	return a.backing.DeleteTokenPermanent(ctx, name)
}

func (a *TokenAdmin) CountActiveTokens(ctx context.Context) (int, error) {
	if err := a.authorized(); err != nil {
		return 0, err
	}
	return a.backing.CountActiveTokens(ctx)
}

func (a *TokenAdmin) BootstrapToken(ctx context.Context, name, token string, passwordConfigured bool) error {
	if err := a.authorized(); err != nil {
		return err
	}
	return a.backing.BootstrapToken(ctx, name, token, passwordConfigured)
}
