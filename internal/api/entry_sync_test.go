package api

import (
	"context"
	"encoding/json"
	"github.com/huynle/brain-api/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEntrySyncIdentityAndScope(t *testing.T) {
	h := NewHandler(nil)
	scope := func(name string) string {
		req := httptest.NewRequest("GET", "/api/v1/sync/identity?account=forged", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxAuthResult, &AuthResult{Type: "jwt", Name: name, Scope: "admin:*"}))
		w := httptest.NewRecorder()
		h.HandleEntrySyncIdentity(w, req)
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body["scope"]
	}
	alice := scope("alice")
	if alice != scope("alice") || alice == scope("bob") {
		t.Fatal("cache identity is not stable and account-isolated")
	}
	validator := newScopedTokenValidator()
	validator.addToken("reader", "read:*")
	router := NewRouter(config.Config{EnableAuth: true}, WithHandler(h), WithTokenValidator(validator))
	for _, tc := range []struct {
		method, path, token string
		want                int
	}{
		{"GET", "/api/v1/sync/entries", "", 401},
		{"POST", "/api/v1/sync/entries", "reader", 403},
		{"GET", "/api/v1/sync/identity", "reader", 200},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		router.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
}
