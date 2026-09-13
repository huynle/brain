package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestSupervisorTailBoundedAndControlScope(t *testing.T) {
	h := &Handler{bridge: &mockBridgeService{historyOut: []byte(`[{"info":{"id":"m"},"parts":[{"id":"p","type":"text","text":"visible"},{"id":"q","type":"reasoning","text":"private"}]}]`)}}
	r := chi.NewRouter()
	r.Use(RequireScope(ScopeControl))
	r.Get("/runners/{runnerId}/sessions/{sessionId}/tail", h.HandleControlSessionTail)
	// No AuthResult follows existing local/no-auth policy. Explicit grants are
	// covered by the shared RequireScope tests; projection must not proxy a write.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/runners/r/sessions/s/tail?limit=1", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), "visible") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, scope := range []string{"read:*", "runner:*"} {
		w = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/runners/r/sessions/s/tail", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxAuthResult, &AuthResult{Scope: scope}))
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("scope %s: %d", scope, w.Code)
		}
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/runners/r/sessions/s/tail?limit=0", nil))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
