package tenant

import (
	"context"
	"fmt"
)

// BindAuthorized is a trusted boundary seam; callers must already have authorized id.
// Use it for tenant-authorized background or in-process work, never with an ID
// taken from a request/tool argument. It grants no capabilities or ownership.
// Conflicting inherited scope is an error, not authority to switch tenants.
func BindAuthorized(ctx context.Context, id ID) (context.Context, error) {
	if !id.Valid() {
		return nil, ErrNoTenant
	}
	if inherited, ok := From(ctx); ok && inherited != id {
		return nil, fmt.Errorf("tenant: conflicting inherited scope")
	}
	return Into(ctx, id), nil
}
