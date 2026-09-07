package storage

import (
	"context"

	"github.com/huynle/brain-api/internal/auth"
)

// Explicit identity-flow delegates. Their callers enforce the existing login,
// consent, PKCE and refresh-token possession checks. None enumerates identities
// or grants general token administration / deployment-operator authority.
func (c *ControlStore) CreateOAuthClient(ctx context.Context, client *OAuthClient) error {
	return c.backing.CreateOAuthClient(ctx, client)
}
func (c *ControlStore) GetOAuthClient(ctx context.Context, id string) (*OAuthClient, error) {
	return c.backing.GetOAuthClient(ctx, id)
}
func (c *ControlStore) CreateAuthCode(ctx context.Context, code *OAuthAuthCode) error {
	return c.backing.CreateAuthCode(ctx, code)
}
func (c *ControlStore) ConsumeAuthCode(ctx context.Context, code string) (*OAuthAuthCode, error) {
	return c.backing.ConsumeAuthCode(ctx, code)
}
func (c *ControlStore) CreateAccessToken(ctx context.Context, token *OAuthAccessToken) error {
	return c.backing.CreateAccessToken(ctx, token)
}
func (c *ControlStore) SaveAccessToken(ctx context.Context, token, clientID, scope string, expiresAt int64) error {
	return c.backing.SaveAccessToken(ctx, token, clientID, scope, expiresAt)
}
func (c *ControlStore) CreateRefreshToken(ctx context.Context, token *OAuthRefreshToken) error {
	return c.backing.CreateRefreshToken(ctx, token)
}
func (c *ControlStore) ConsumeRefreshToken(ctx context.Context, token string) (*OAuthRefreshToken, error) {
	return c.backing.ConsumeRefreshToken(ctx, token)
}

// MarkInstallClaimed is system-wide initialization, not a request operation.
func (c *ControlStore) MarkInstallClaimed(ctx context.Context, cap auth.DeploymentOperator) error {
	if !cap.Valid() {
		return auth.ErrOperatorRequired
	}
	return c.backing.MarkInstallClaimed(ctx)
}
