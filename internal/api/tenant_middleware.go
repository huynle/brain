package api

import (
	"context"
	"net/http"

	"github.com/huynle/brain-api/internal/tenant"
)

// TenantScope resolves scope after Auth, independently of capabilities.
// Single mode maps the existing authenticated identities to the local tenant.
// Only auth-disabled single mode synthesizes an explicit local principal.
// Multi resolution requires a tenant supplied by trusted authentication; current
// credential validators do not supply one and operational multi remains disabled.
// No header, URL, body, or MCP argument participates in this decision.
func TenantScope(mode tenant.Mode, authEnabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, _ := AuthResultFromContext(r.Context())
			var resolved AuthResult
			if principal != nil {
				resolved = *principal // Do not mutate a principal shared by callers.
			}
			reject := func() {
				WriteError(w, http.StatusUnauthorized, "Unauthorized", "Missing or conflicting trusted tenant principal")
			}
			switch mode {
			case "", tenant.ModeSingle:
				if principal == nil {
					if authEnabled {
						reject()
						return
					}
					// Auth-disabled requests historically bypassed all scope checks.
					resolved = AuthResult{Type: "local", Name: "local", Scope: "admin:*"}
				}
				if resolved.Tenant != (tenant.ID{}) && resolved.Tenant != tenant.Local {
					reject()
					return
				}
				resolved.Tenant = tenant.Local
			case tenant.ModeMulti:
				if principal == nil || !resolved.Tenant.Valid() {
					reject()
					return
				}
				switch resolved.Type {
				case "api_token", "oauth", "jwt":
				default:
					reject()
					return
				}
			default:
				reject()
				return
			}
			ctx, err := tenant.BindAuthorized(r.Context(), resolved.Tenant)
			if err != nil {
				reject()
				return
			}
			ctx = context.WithValue(ctx, ctxAuthResult, &resolved)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
