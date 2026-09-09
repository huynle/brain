package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/internal/webui"
)

func localCORSTestHandler(t *testing.T, opts ServerOptions) http.Handler {
	t.Helper()
	opts.BrainDir = t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	h, _, cleanup, err := buildHTTPHandler(ctx, opts)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); cleanup() })
	return h
}

func assertNoCORSGrants(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for key := range w.Header() {
		if strings.HasPrefix(key, "Access-Control-") {
			t.Errorf("unexpected %s: %q", key, w.Header().Values(key))
		}
	}
}

func TestBuildHTTPHandler_EmptyCORSNoWildcardFallback(t *testing.T) {
	h := localCORSTestHandler(t, ServerOptions{Host: "localhost"})
	for _, path := range []string{"/api/v1/tasks", "/mcp", "/"} {
		for _, origin := range []string{"https://hostile.example", "null"} {
			for _, method := range []string{http.MethodGet, http.MethodOptions} {
				t.Run(method+path+"/"+origin, func(t *testing.T) {
					r := httptest.NewRequest(method, path, nil)
					r.Header.Set("Origin", origin)
					r.Header.Set("Access-Control-Request-Method", "POST")
					r.Header.Set("Access-Control-Request-Headers", "Authorization, Mcp-Session-Id")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					assertNoCORSGrants(t, w)
				})
			}
		}
	}
}

func TestBuildHTTPHandler_DefaultLocalPWACompatibility(t *testing.T) {
	defaults := config.DefaultConfig().Server
	h := localCORSTestHandler(t, ServerOptions{Host: defaults.Host, EnableAuth: defaults.EnableAuth, CORSOrigin: defaults.CORSOrigin})
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://localhost:3333"+path, nil)
		r.Header.Set("Origin", "http://localhost:3333")
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
		assertNoCORSGrants(t, w)
		return w
	}
	api := get("/api/v1/tasks")
	var tasks struct {
		Projects []string `json:"projects"`
	}
	if !strings.Contains(api.Header().Get("Content-Type"), "application/json") || json.Unmarshal(api.Body.Bytes(), &tasks) != nil {
		t.Fatalf("API returned non-JSON/SPA shell: %s", api.Body.String())
	}
	// A same-origin JSON mutation needs neither CORS grants nor new login
	// provisioning. Read it back through the real indexed service as well.
	r := httptest.NewRequest(http.MethodPost, "http://localhost:3333/api/v1/entries", strings.NewReader(`{"type":"summary","title":"Local CORS compatibility","content":"Saved from the local PWA","project":"cors-test"}`))
	r.Header.Set("Origin", "http://localhost:3333")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assertNoCORSGrants(t, w)
	if w.Code != http.StatusCreated {
		t.Fatalf("same-origin save: %d %s", w.Code, w.Body.String())
	}
	var created types.CreateEntryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatalf("missing saved ID: %s", w.Body.String())
	}
	recalled := get("/api/v1/entries/" + created.ID)
	if !strings.Contains(recalled.Body.String(), "Saved from the local PWA") {
		t.Fatalf("saved content not recalled: %s", recalled.Body.String())
	}
	root := get("/")
	if !strings.Contains(root.Header().Get("Content-Type"), "text/html") {
		t.Fatal("root is not HTML")
	}
	for _, path := range []string{"/tasks", "/auth/callback"} {
		if got := get(path).Body.String(); got != root.Body.String() {
			t.Fatalf("navigation %s did not return the SPA shell", path)
		}
	}
	if !webui.IsBuilt() {
		t.Skip("PWA assets not built; run npm ci && npm run build in web, then rerun this test")
	}
	if !strings.Contains(root.Body.String(), `id="root"`) {
		t.Fatal("missing React mount point")
	}
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^" ]+\.(?:js|css))"`).FindAllStringSubmatch(root.Body.String(), -1)
	if len(assets) < 2 {
		t.Fatalf("expected JS and CSS asset references in shell: %s", root.Body.String())
	}
	for _, asset := range assets {
		w := get(asset[1])
		if w.Body.Len() == 0 || strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("asset %s returned empty content or SPA fallback", asset[1])
		}
	}
	manifest := get("/manifest.webmanifest")
	var m map[string]any
	if err := json.Unmarshal(manifest.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["start_url"] != "/" {
		t.Fatalf("manifest start_url = %v", m["start_url"])
	}
}

func TestBuildHTTPHandler_ExplicitCORSPolicyPreserved(t *testing.T) {
	for _, policy := range []string{"https://trusted.example", "*"} {
		t.Run(policy, func(t *testing.T) {
			h := localCORSTestHandler(t, ServerOptions{Host: "localhost", CORSOrigin: policy})
			for _, method := range []string{http.MethodGet, http.MethodOptions} {
				r := httptest.NewRequest(method, "/api/v1/tasks", nil)
				r.Header.Set("Origin", "https://trusted.example")
				r.Header.Set("Access-Control-Request-Method", "POST")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if got := w.Header().Get("Access-Control-Allow-Origin"); got != policy {
					t.Errorf("origin = %q, want %q", got, policy)
				}
				wantCredentials := "true"
				if policy == "*" {
					wantCredentials = ""
				}
				if got := w.Header().Get("Access-Control-Allow-Credentials"); got != wantCredentials {
					t.Errorf("credentials = %q, want %q", got, wantCredentials)
				}
			}
		})
	}
}
