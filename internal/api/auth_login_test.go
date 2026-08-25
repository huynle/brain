package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
)

type fakeVerifier struct{ ok, configured bool }

func (f fakeVerifier) Verify(u, p string) bool { return f.ok }
func (f fakeVerifier) Configured() bool        { return f.configured }
func (f fakeVerifier) Username() string        { return "admin" }

type fakeTokenStore struct {
	access  map[string]*storage.OAuthAccessToken
	refresh map[string]*storage.OAuthRefreshToken
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{
		access:  map[string]*storage.OAuthAccessToken{},
		refresh: map[string]*storage.OAuthRefreshToken{},
	}
}

func (s *fakeTokenStore) CreateAccessToken(_ context.Context, t *storage.OAuthAccessToken) error {
	s.access[t.Token] = t
	return nil
}
func (s *fakeTokenStore) CreateRefreshToken(_ context.Context, t *storage.OAuthRefreshToken) error {
	s.refresh[t.Token] = t
	return nil
}
func (s *fakeTokenStore) ConsumeRefreshToken(_ context.Context, tok string) (*storage.OAuthRefreshToken, error) {
	t, ok := s.refresh[tok]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	delete(s.refresh, tok)
	return t, nil
}

func loginHandler(t *testing.T, ok, configured bool) (*Handler, *fakeTokenStore) {
	t.Helper()
	store := newFakeTokenStore()
	h := NewHandler(nil,
		WithCredentialVerifier(fakeVerifier{ok: ok, configured: configured}),
		WithPasswordTokenStore(store),
	)
	return h, store
}

func postJSON(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	r.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestAuthLogin_Success(t *testing.T) {
	h, store := loginHandler(t, true, true)
	w := postJSON(h.HandleAuthLogin, `{"username":"admin","password":"pw"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("expected access and refresh tokens")
	}
	if len(store.access) != 1 || len(store.refresh) != 1 {
		t.Errorf("tokens not persisted: access=%d refresh=%d", len(store.access), len(store.refresh))
	}
}

func TestAuthLogin_BadCredentials(t *testing.T) {
	h, _ := loginHandler(t, false, true)
	w := postJSON(h.HandleAuthLogin, `{"username":"admin","password":"wrong"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAuthLogin_NotConfigured(t *testing.T) {
	h, _ := loginHandler(t, true, false)
	w := postJSON(h.HandleAuthLogin, `{"username":"admin","password":"pw"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when password login disabled", w.Code)
	}
}

func TestAuthLogin_Lockout(t *testing.T) {
	h, _ := loginHandler(t, false, true)
	for i := 0; i < maxLoginFails; i++ {
		_ = postJSON(h.HandleAuthLogin, `{"username":"admin","password":"x"}`)
	}
	// Next attempt from same IP is locked out.
	w := postJSON(h.HandleAuthLogin, `{"username":"admin","password":"x"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after %d fails", w.Code, maxLoginFails)
	}
}

func TestAuthRefresh_RotatesAndRejectsReuse(t *testing.T) {
	h, store := loginHandler(t, true, true)
	// Login to obtain a refresh token.
	lw := postJSON(h.HandleAuthLogin, `{"username":"admin","password":"pw"}`)
	var login tokenResponse
	_ = json.Unmarshal(lw.Body.Bytes(), &login)

	// Refresh with it → new pair.
	rw := postJSON(h.HandleAuthRefresh, fmt.Sprintf(`{"refresh_token":%q}`, login.RefreshToken))
	if rw.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 (body %s)", rw.Code, rw.Body.String())
	}
	var refreshed tokenResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatal("expected refreshed access and refresh tokens")
	}
	if refreshed.AccessToken == login.AccessToken {
		t.Fatal("expected refresh to return a new access token")
	}
	if _, ok := store.refresh[login.RefreshToken]; ok {
		t.Fatal("expected old refresh token to be invalidated")
	}
	// Reusing the now-consumed refresh token must fail (single-use rotation).
	again := postJSON(h.HandleAuthRefresh, fmt.Sprintf(`{"refresh_token":%q}`, login.RefreshToken))
	if again.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh status = %d, want 401", again.Code)
	}
}

func TestAuthRefresh_Invalid(t *testing.T) {
	h, _ := loginHandler(t, true, true)
	w := postJSON(h.HandleAuthRefresh, `{"refresh_token":"does-not-exist"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAuthLogout_Always204(t *testing.T) {
	h, _ := loginHandler(t, true, true)
	w := postJSON(h.HandleAuthLogout, `{"refresh_token":"anything"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}
