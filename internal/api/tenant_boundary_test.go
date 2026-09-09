package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

// This validator and credential-to-tenant mapping are TEST-ONLY fixtures. The
// real Auth middleware protects AuthResult; it does not yet install tenant
// context. This test is NOT evidence of deployed multi-tenant data isolation.
type tenantFixtureValidator struct{}

func (tenantFixtureValidator) ValidateToken(_ context.Context, token string) (*storage.Token, error) {
	if token == "credential-a" {
		return &storage.Token{Name: "account-a", Scope: "read"}, nil
	}
	if token == "credential-b" {
		return &storage.Token{Name: "account-b", Scope: "read"}, nil
	}
	return nil, fmt.Errorf("invalid fixture credential")
}

func TestTenantSelectorsCannotReplaceAuthenticatedScopeFixture(t *testing.T) {
	accounts := map[string]tenant.ID{"account-a": tenant.MustParse("tenant-a"), "account-b": tenant.MustParse("tenant-b")}
	for _, tc := range []struct {
		name, credential, foreign, want string
		status                          int
	}{
		{"url-header-json-foreign-b", "credential-a", "tenant-b", "tenant-a", http.StatusOK},
		{"url-header-json-foreign-a", "credential-b", "tenant-a", "tenant-b", http.StatusOK},
		{"valid-selector-no-credential", "", "tenant-b", "", http.StatusUnauthorized},
		{"valid-selector-invalid-credential", "invalid", "tenant-b", "", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := api.Auth(true, tenantFixtureValidator{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				auth, ok := api.AuthResultFromContext(r.Context())
				if !ok {
					t.Fatal("missing authenticated result")
				}
				before := *auth
				// This mapping, not Parse, supplies the fixture's authenticated identity.
				trusted := accounts[auth.Name]
				if !trusted.Valid() {
					t.Fatal("fixture has no authenticated tenant")
				}
				var body struct {
					Tenant tenant.ID `json:"tenant"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				for _, selector := range []string{strings.TrimPrefix(r.URL.Path, "/tenants/"), r.URL.Query().Get("tenant"), r.Header.Get("X-Tenant-ID"), body.Tenant.String()} {
					parsed, err := tenant.Parse(selector)
					if err != nil || parsed.String() != tc.foreign || parsed == trusted {
						t.Fatalf("foreign selector was not a valid different ID: %v, %v", parsed, err)
					}
				}
				after, ok := api.AuthResultFromContext(r.Context())
				if !ok || *after != before || after.Scope != "read" || accounts[after.Name] != trusted {
					t.Fatal("selectors replaced authenticated scope")
				}
				_, _ = w.Write([]byte(trusted.String()))
			}))
			req := httptest.NewRequest(http.MethodPost, "/tenants/"+tc.foreign+"?tenant="+tc.foreign, strings.NewReader(`{"tenant":"`+tc.foreign+`"}`))
			req.Header.Set("X-Tenant-ID", tc.foreign)
			if tc.credential != "" {
				req.Header.Set("Authorization", "Bearer "+tc.credential)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
			}
			if tc.status == http.StatusOK && recorder.Body.String() != tc.want {
				t.Fatalf("tenant = %q, want %q", recorder.Body.String(), tc.want)
			}
			if called != (tc.status == http.StatusOK) {
				t.Fatal("unauthenticated request reached fixture")
			}
		})
	}
}
