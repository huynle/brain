package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
)

// Temporary write:* bridge (decision nezg1ru5): content reads + the P6.6
// content-write route class (e48ffe22), never administration or control.

func scopeStatus(t *testing.T, mw func(http.Handler) http.Handler, authType, scope string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxAuthResult, &AuthResult{Type: authType, Name: "n", Scope: scope}))
	rec := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestWriteBridgeScopeRequireScope(t *testing.T) {
	read := RequireScope("admin:*", "runner:*", "read:*")
	contentWrite := RequireScope("admin:*", ScopeWriteBridge)
	adminOnly := RequireScope("admin:*")
	runnerOnly := RequireScope("admin:*", "runner:*")
	control := RequireScope(ScopeControl)
	for _, tc := range []struct {
		name     string
		mw       func(http.Handler) http.Handler
		authType string
		scope    string
		want     int
	}{
		// write:* = content reads + content writes.
		{"write reaches read routes", read, "api_token", ScopeWriteBridge, http.StatusOK},
		{"write reaches content writes", contentWrite, "api_token", ScopeWriteBridge, http.StatusOK},
		{"write refused administration", adminOnly, "api_token", ScopeWriteBridge, http.StatusForbidden},
		{"write refused runner routes", runnerOnly, "api_token", ScopeWriteBridge, http.StatusForbidden},
		{"write refused control", control, "api_token", ScopeWriteBridge, http.StatusForbidden},
		// Existing scopes are unchanged on a content-write route.
		{"admin reaches content writes", contentWrite, "api_token", "admin:*", http.StatusOK},
		{"jwt admin reaches content writes", contentWrite, "jwt", "admin:*", http.StatusOK},
		{"read refused content writes", contentWrite, "api_token", "read:*", http.StatusForbidden},
		{"runner refused content writes", contentWrite, "api_token", "runner:*", http.StatusForbidden},
		{"control refused content writes", contentWrite, "api_token", ScopeControl, http.StatusForbidden},
		{"oauth legacy mcp still reaches content writes", contentWrite, "oauth", "mcp", http.StatusOK},
		{"oauth read:* still refused content writes", contentWrite, "oauth", "read:*", http.StatusForbidden},
		// The bridge is an API-token scope only: an OAuth grant naming it
		// gains nothing it did not have before.
		{"oauth write:* literal refused content writes", contentWrite, "oauth", ScopeWriteBridge, http.StatusForbidden},
		{"oauth write:* literal refused read routes", read, "oauth", ScopeWriteBridge, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, body := scopeStatus(t, tc.mw, tc.authType, tc.scope); got != tc.want {
				t.Fatalf("status = %d, want %d: %s", got, tc.want, body)
			}
		})
	}
}

func TestWriteBridgeRefusal(t *testing.T) {
	// Guards routes that carry no RequireScope on main (webhooks, event
	// ingest): only write:* is refused; every other principal is unchanged.
	for _, tc := range []struct {
		authType, scope string
		want            int
	}{
		{"api_token", ScopeWriteBridge, http.StatusForbidden},
		{"api_token", "admin:*", http.StatusOK},
		{"api_token", "runner:*", http.StatusOK},
		{"api_token", "read:*", http.StatusOK},
		{"api_token", ScopeControl, http.StatusOK},
		{"jwt", "admin:*", http.StatusOK},
		{"oauth", "mcp", http.StatusOK},
		{"oauth", ScopeWriteBridge, http.StatusOK},
	} {
		got, body := scopeStatus(t, RefuseWriteBridge, tc.authType, tc.scope)
		if got != tc.want {
			t.Errorf("%s %q: status = %d, want %d: %s", tc.authType, tc.scope, got, tc.want, body)
		}
		if got == http.StatusForbidden && !strings.Contains(body, "Token scope") {
			t.Errorf("refusal body %q lacks the scope message", body)
		}
	}
	// No auth result (auth off): pass through like RequireScope.
	rec := httptest.NewRecorder()
	RefuseWriteBridge(okHandler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("no auth result: %d", rec.Code)
	}
}

func TestCreateTokenAcceptsWriteBridgeScope(t *testing.T) {
	if !ValidScopes[ScopeWriteBridge] {
		t.Fatal("write:* is not a valid token scope")
	}
	var stored string
	mock := &mockTokenService{
		createTokenFunc: func(_ context.Context, _, _, scope string) error { stored = scope; return nil },
		getTokenByNameFunc: func(_ context.Context, name string) (*storage.Token, error) {
			return nil, fmt.Errorf("token not found: %s", name)
		},
	}
	body, _ := json.Marshal(map[string]string{"name": "task-agent", "scope": ScopeWriteBridge})
	rec := httptest.NewRecorder()
	newTokenTestRouter(mock).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated || stored != ScopeWriteBridge {
		t.Fatalf("create write:* token: %d stored=%q %s", rec.Code, stored, rec.Body.String())
	}
	body, _ = json.Marshal(map[string]string{"name": "x", "scope": "bogus"})
	rec = httptest.NewRecorder()
	newTokenTestRouter(mock).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "write:*") {
		t.Fatalf("invalid scope error should list write:*: %d %s", rec.Code, rec.Body.String())
	}
}
