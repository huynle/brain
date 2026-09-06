package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
)

func TestCORSEmptyPolicyNoGrants(t *testing.T) {
	for _, origin := range []string{"", "http://localhost:3333", "https://hostile.example", "null"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodOptions} {
			t.Run(method+"/"+origin, func(t *testing.T) {
				called := false
				h := CORS(config.Config{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.WriteHeader(http.StatusOK)
				}))
				r := httptest.NewRequest(method, "/api/v1/entries", nil)
				r.Header.Set("Origin", origin)
				if method == http.MethodOptions {
					r.Header.Set("Access-Control-Request-Method", "POST")
					r.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				for key := range w.Header() {
					if strings.HasPrefix(key, "Access-Control-") {
						t.Errorf("empty policy emitted %s: %q", key, w.Header().Values(key))
					}
				}
				if method != http.MethodOptions && (!called || w.Code != http.StatusOK) {
					t.Fatal("empty CORS must not block ordinary requests")
				}
			})
		}
	}
}
