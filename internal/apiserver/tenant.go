package apiserver

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/tenant"
)

// backgroundTenantContext is the deployment-authorized local bootstrap seam.
// Install it before services start, not from event payloads. This only scopes
// context-aware work; storage/indexer enforcement belongs to later phases.
func backgroundTenantContext(ctx context.Context, mode tenant.Mode) (context.Context, error) {
	parsed, err := tenant.ParseMode(string(mode))
	if err != nil {
		return nil, err
	}
	if parsed == tenant.ModeMulti {
		return nil, fmt.Errorf("tenant: operational multi mode is disabled until tenant enforcement is complete")
	}
	return tenant.BindAuthorized(ctx, tenant.Local)
}
