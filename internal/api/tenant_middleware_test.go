package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantScopeResolution(t *testing.T) {
	a, b := tenant.MustParse("tenant-a"), tenant.MustParse("tenant-b")
	for _, tc := range []struct {
		name      string
		mode      tenant.Mode
		enabled   bool
		principal *AuthResult
		inherited tenant.ID
		want      tenant.ID
		status    int
	}{
		{"local", tenant.ModeSingle, false, nil, tenant.ID{}, tenant.Local, 204},
		{"single-authenticated", tenant.ModeSingle, true, &AuthResult{Type: "oauth", Scope: "mcp"}, tenant.ID{}, tenant.Local, 204},
		{"single-no-principal", tenant.ModeSingle, true, nil, tenant.ID{}, tenant.ID{}, 401},
		{"single-foreign-principal", tenant.ModeSingle, true, &AuthResult{Type: "api_token", Tenant: a}, tenant.ID{}, tenant.ID{}, 401},
		{"multi", tenant.ModeMulti, true, &AuthResult{Type: "oauth", Tenant: a, Scope: "mcp"}, tenant.ID{}, a, 204},
		{"multi-same-inherited", tenant.ModeMulti, true, &AuthResult{Type: "api_token", Tenant: a, Scope: "read:*"}, a, a, 204},
		{"multi-conflict", tenant.ModeMulti, true, &AuthResult{Type: "api_token", Tenant: a}, b, tenant.ID{}, 401},
		{"single-conflict", tenant.ModeSingle, false, nil, b, tenant.ID{}, 401},
		{"multi-no-principal", tenant.ModeMulti, true, nil, a, tenant.ID{}, 401},
		{"multi-nil-principal", tenant.ModeMulti, true, nil, tenant.ID{}, tenant.ID{}, 401},
		{"multi-missing-tenant", tenant.ModeMulti, true, &AuthResult{Type: "jwt", Scope: "admin:*"}, a, tenant.ID{}, 401},
		{"multi-empty-principal", tenant.ModeMulti, true, &AuthResult{Tenant: a}, tenant.ID{}, tenant.ID{}, 401},
		{"multi-local-principal", tenant.ModeMulti, true, &AuthResult{Type: "local", Tenant: tenant.Local}, tenant.ID{}, tenant.ID{}, 401},
		{"multi-disabled", tenant.ModeMulti, false, nil, tenant.ID{}, tenant.ID{}, 401},
		{"invalid-mode", "invalid", false, nil, tenant.ID{}, tenant.ID{}, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tenant.Into(context.Background(), tc.inherited)
			ctx = context.WithValue(ctx, ctxAuthResult, tc.principal)
			var before AuthResult
			if tc.principal != nil {
				before = *tc.principal
			}
			called := false
			h := TenantScope(tc.mode, tc.enabled)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				id, ok := tenant.From(r.Context())
				if !ok || id != tc.want {
					t.Errorf("tenant = %v, %v; want %v", id, ok, tc.want)
				}
				p, ok := AuthResultFromContext(r.Context())
				if !ok || p == nil || p.Tenant != tc.want {
					t.Errorf("missing resolved principal: %+v", p)
				}
				if tc.principal != nil && (p.Type != before.Type || p.Scope != before.Scope || p.Name != before.Name || p.ClientID != before.ClientID) {
					t.Error("changed authenticated capabilities/identity")
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"tenant":"tenant-b"}` {
					t.Error("middleware consumed request body")
				}
				w.WriteHeader(204)
			}))
			r := httptest.NewRequest("POST", "/tenants/tenant-b?tenant=tenant-b", strings.NewReader(`{"tenant":"tenant-b"}`)).WithContext(ctx)
			r.Header.Set("X-Tenant-ID", "tenant-b")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Errorf("status = %d, want %d", w.Code, tc.status)
			}
			if called != (tc.status == 204) {
				t.Error("wrong downstream dispatch")
			}
			if tc.principal != nil && *tc.principal != before {
				t.Error("mutated shared principal")
			}
		})
	}
}

func TestTenantScopePreservesCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principal *AuthResult
		enabled   bool
		want      int
	}{
		{"local-control", nil, false, 204},
		{"oauth-mcp-not-control", &AuthResult{Type: "oauth", Scope: "mcp"}, true, 403},
		{"read-not-control", &AuthResult{Type: "api_token", Scope: "read:*"}, true, 403},
		{"admin-control", &AuthResult{Type: "api_token", Scope: "admin:*"}, true, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := TenantScope(tenant.ModeSingle, tc.enabled)(RequireScope(ScopeControl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := tenant.From(r.Context()); !ok {
					t.Error("missing tenant")
				}
				w.WriteHeader(204)
			})))
			r := httptest.NewRequest("GET", "/", nil)
			if tc.principal != nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxAuthResult, tc.principal))
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestRouterRejectsForeignInheritedTenant(t *testing.T) {
	router := NewRouter(config.Config{}, WithHandler(NewHandler(nil)))
	r := httptest.NewRequest("GET", "/api/v1/server/requests/recent", nil)
	r = r.WithContext(tenant.Into(r.Context(), tenant.MustParse("foreign")))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("status = %d; want tenant rejection before handler", w.Code)
	}
}
