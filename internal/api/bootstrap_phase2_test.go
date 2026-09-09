package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestPhase2BootstrapRejectsRemotePeer(t *testing.T) {
	t.Setenv("BRAIN_ALLOW_REMOTE_BOOTSTRAP", "")
	s, err := storage.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	h := NewHandler(&mockBrainService{}, WithTokenService(singleModeTokens(t, s)))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/tokens/bootstrap", strings.NewReader(`{"name":"remote"}`))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	w := httptest.NewRecorder()
	h.HandleBootstrapToken(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote bootstrap status = %d, want 403", w.Code)
	}
}

func singleModeTokens(t *testing.T, owner *storage.StorageLayer) *storage.SingleModeTokenStore {
	t.Helper()
	compat, err := owner.SingleModeTokens(tenant.ModeSingle)
	if err != nil {
		t.Fatal(err)
	}
	return compat
}
