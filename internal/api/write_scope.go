package api

import (
	"fmt"
	"net/http"
)

// ScopeWriteBridge is the API-token scope for content reads plus content
// writes, without administration or control (e.g. an OpenCode runner's task
// agents writing Brain content through the hosted /mcp with a narrow
// credential instead of admin:*).
//
// TEMPORARY BRIDGE (decision nezg1ru5). Its write set is exactly the P6.6
// content-write route class (commit e48ffe22, ScopeOAuthContentWrite /
// oauthContentWriteRoutes): entry, attachment, link, bulk-job and sync
// writes, reminders, and task/feature run, dispatch and resume. Every other
// admin:* route, runner route and control route refuses it. When the
// multi-tenant line merges, fold this scope into the P6.6 route classes
// (mark those routes once, admit both) and delete the bridge.
//
// Reach is decided in RequireScope: a route admits write:* when it admits
// read:* (content reads) or names ScopeWriteBridge (content writes). Routes
// that carry no RequireScope on main use RefuseWriteBridge instead.
// Pinned by internal/apiserver TestWriteBridgeRouteInventory.
const ScopeWriteBridge = "write:*"

// writeBridgeSatisfies reports whether an API-token write:* scope reaches a
// route with the given allowed scopes. OAuth grants never use this path.
func writeBridgeSatisfies(allowed map[string]bool) bool {
	return allowed["read:*"] || allowed[ScopeWriteBridge]
}

// RefuseWriteBridge refuses a non-OAuth write:* principal and passes every
// other principal unchanged. It guards routes that carry no RequireScope on
// main but that P6.6 classifies as administration (webhooks, event
// ingest), so the bridge cannot reach them while read:*'s reach stays as is.
func RefuseWriteBridge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth, ok := AuthResultFromContext(r.Context()); ok && auth.Type != "oauth" && auth.Scope == ScopeWriteBridge {
			WriteError(w, http.StatusForbidden, "Forbidden",
				fmt.Sprintf("Token scope %q insufficient for this administration route", auth.Scope))
			return
		}
		next.ServeHTTP(w, r)
	})
}
