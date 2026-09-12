package storage

import (
	"context"

	"github.com/huynle/brain-api/internal/auth"
)

// Explicit identity-flow delegates. Their callers enforce the existing login,
// consent, PKCE and refresh-token possession checks. None enumerates identities
// or grants general token administration / deployment-operator authority.
func (c *ControlStore) CreateOAuthClient(ctx context.Context, client *OAuthClient) error {
	return (identityStore{db: c.backing.db}).createOAuthClient(ctx, client)
}
func (c *ControlStore) GetOAuthClient(ctx context.Context, id string) (*OAuthClient, error) {
	return (identityStore{db: c.backing.db}).getOAuthClient(ctx, id)
}
func (c *ControlStore) CreateAuthCode(ctx context.Context, code *OAuthAuthCode) error {
	return (identityStore{db: c.backing.db}).createAuthCode(ctx, code)
}
func (c *ControlStore) ConsumeAuthCode(ctx context.Context, code string) (*OAuthAuthCode, error) {
	return (identityStore{db: c.backing.db}).consumeAuthCode(ctx, code)
}
func (c *ControlStore) CreateAccessToken(ctx context.Context, token *OAuthAccessToken) error {
	return (identityStore{db: c.backing.db}).createAccessToken(ctx, token)
}
func (c *ControlStore) SaveAccessToken(ctx context.Context, token, clientID, scope string, expiresAt int64) error {
	return (identityStore{db: c.backing.db}).saveAccessToken(ctx, token, clientID, scope, expiresAt)
}
func (c *ControlStore) CreateRefreshToken(ctx context.Context, token *OAuthRefreshToken) error {
	return (identityStore{db: c.backing.db}).createRefreshToken(ctx, token)
}
func (c *ControlStore) ConsumeRefreshToken(ctx context.Context, token string) (*OAuthRefreshToken, error) {
	return (identityStore{db: c.backing.db}).consumeRefreshToken(ctx, token)
}

// MarkInstallClaimed is system-wide initialization, not a request operation.
func (c *ControlStore) MarkInstallClaimed(ctx context.Context, cap auth.DeploymentOperator) error {
	if !cap.Valid() {
		return auth.ErrOperatorRequired
	}
	return (identityStore{db: c.backing.db}).markInstallClaimed(ctx)
}
