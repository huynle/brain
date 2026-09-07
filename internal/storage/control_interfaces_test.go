package storage_test

import (
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/oauth"
	"github.com/huynle/brain-api/internal/storage"
)

// Phase 2 can use the existing interfaces without giving them raw storage.
var (
	_ api.TokenValidator            = (*storage.ControlStore)(nil)
	_ api.OAuthAccessTokenValidator = (*storage.ControlStore)(nil)
	_ api.TokenService              = (*storage.TokenAdmin)(nil)
	_ api.TokenService              = (*storage.SingleModeTokenStore)(nil)
	_ api.PasswordTokenStore        = (*storage.ControlStore)(nil)
	_ oauth.PersistentBackend       = (*storage.ControlStore)(nil)
	_ oauth.AccessTokenStore        = (*storage.ControlStore)(nil)
)
