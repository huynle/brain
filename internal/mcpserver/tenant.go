package mcpserver

import (
	"context"

	"github.com/huynle/brain-api/internal/tenant"
)

// localTenantContext authorizes local stdio process work only. It says nothing
// about the remote API account: outbound calls still authenticate independently.
// Never use tool arguments or the remote URL to select this scope.
func localTenantContext(ctx context.Context) (context.Context, error) {
	return tenant.BindAuthorized(ctx, tenant.Local)
}
