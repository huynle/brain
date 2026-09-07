package tenant

import (
	"context"
	"errors"
)

type ctxKey struct{}

// ErrNoTenant signals that a context has no valid tenant scope.
var ErrNoTenant = errors.New("tenant: no tenant in context")

// Into carries scope, not authorization, in a child of ctx. Even an invalid ID
// is stored so that it masks inherited scope rather than falling back to it.
func Into(ctx context.Context, id ID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From returns valid tenant scope, never an authorization grant. Missing, zero,
// or invalid values return the zero ID and false, with no local fallback.
func From(ctx context.Context) (ID, bool) {
	id, ok := ctx.Value(ctxKey{}).(ID)
	if !ok || !id.Valid() {
		return ID{}, false
	}
	return id, true
}
