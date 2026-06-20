package oauth

import (
	"context"
	"log/slog"
	"time"

	"github.com/huynle/brain-api/internal/storage"
)

// PersistentBackend is the subset of *storage.StorageLayer that the SQLite-backed
// OAuth flow store depends on. Declaring it as an interface keeps the dependency
// narrow and makes PersistentStore unit-testable.
type PersistentBackend interface {
	CreateOAuthClient(ctx context.Context, c *storage.OAuthClient) error
	GetOAuthClient(ctx context.Context, id string) (*storage.OAuthClient, error)
	CreateAuthCode(ctx context.Context, code *storage.OAuthAuthCode) error
	ConsumeAuthCode(ctx context.Context, code string) (*storage.OAuthAuthCode, error)
	CreateRefreshToken(ctx context.Context, t *storage.OAuthRefreshToken) error
	ConsumeRefreshToken(ctx context.Context, t string) (*storage.OAuthRefreshToken, error)
}

// PersistentStore implements FlowStore backed by SQLite so OAuth clients, auth
// codes, and refresh tokens survive server restarts. Without this, the OAuth
// flow used in-memory maps that were wiped on every restart, producing
// "unknown client_id" for already-registered clients (e.g. the Claude
// connector and the PWA).
type PersistentStore struct {
	db PersistentBackend
}

// NewPersistentStore returns a FlowStore backed by the given storage layer.
func NewPersistentStore(db PersistentBackend) *PersistentStore {
	return &PersistentStore{db: db}
}

var _ FlowStore = (*PersistentStore)(nil)

func (s *PersistentStore) GetClient(id string) (*Client, bool) {
	c, err := s.db.GetOAuthClient(context.Background(), id)
	if err != nil || c == nil {
		return nil, false
	}
	return &Client{
		ClientID:      c.ClientID,
		ClientSecret:  c.ClientSecret,
		ClientName:    c.ClientName,
		RedirectURIs:  c.RedirectURIs,
		GrantTypes:    c.GrantTypes,
		ResponseTypes: c.ResponseTypes,
		Scope:         c.Scope,
		CreatedAt:     time.Unix(c.CreatedAt, 0),
	}, true
}

func (s *PersistentStore) SaveClient(c *Client) {
	if err := s.db.CreateOAuthClient(context.Background(), &storage.OAuthClient{
		ClientID:      c.ClientID,
		ClientSecret:  c.ClientSecret,
		ClientName:    c.ClientName,
		RedirectURIs:  c.RedirectURIs,
		GrantTypes:    c.GrantTypes,
		ResponseTypes: c.ResponseTypes,
		Scope:         c.Scope,
		CreatedAt:     c.CreatedAt.Unix(),
	}); err != nil {
		slog.Error("oauth: failed to persist client registration", "client_id", c.ClientID, "error", err)
	}
}

func (s *PersistentStore) SaveAuthCode(ac *AuthCode) {
	if err := s.db.CreateAuthCode(context.Background(), &storage.OAuthAuthCode{
		Code:                ac.Code,
		ClientID:            ac.ClientID,
		RedirectURI:         ac.RedirectURI,
		Scope:               ac.Scope,
		CodeChallenge:       ac.CodeChallenge,
		CodeChallengeMethod: ac.CodeChallengeMethod,
		ExpiresAt:           ac.ExpiresAt.Unix(),
	}); err != nil {
		slog.Error("oauth: failed to persist auth code", "error", err)
	}
}

func (s *PersistentStore) ConsumeAuthCode(code string) (*AuthCode, bool) {
	c, err := s.db.ConsumeAuthCode(context.Background(), code)
	if err != nil || c == nil {
		return nil, false
	}
	return &AuthCode{
		Code:                c.Code,
		ClientID:            c.ClientID,
		RedirectURI:         c.RedirectURI,
		Scope:               c.Scope,
		CodeChallenge:       c.CodeChallenge,
		CodeChallengeMethod: c.CodeChallengeMethod,
		ExpiresAt:           time.Unix(c.ExpiresAt, 0),
	}, true
}

func (s *PersistentStore) SaveRefreshToken(token string, entry *RefreshEntry) {
	if err := s.db.CreateRefreshToken(context.Background(), &storage.OAuthRefreshToken{
		Token:     token,
		ClientID:  entry.ClientID,
		Scope:     entry.Scope,
		ExpiresAt: entry.ExpiresAt.Unix(),
	}); err != nil {
		slog.Error("oauth: failed to persist refresh token", "error", err)
	}
}

func (s *PersistentStore) ConsumeRefreshToken(token string) (*RefreshEntry, bool) {
	t, err := s.db.ConsumeRefreshToken(context.Background(), token)
	if err != nil || t == nil {
		return nil, false
	}
	return &RefreshEntry{
		ClientID:  t.ClientID,
		Scope:     t.Scope,
		ExpiresAt: time.Unix(t.ExpiresAt, 0),
	}, true
}
