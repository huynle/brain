package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustSub(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	return sub
}

func findAsset(t *testing.T, sub fs.FS) string {
	t.Helper()
	var found string
	_ = fs.WalkDir(sub, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil //nolint:nilerr
		}
		if strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css") {
			found = p
		}
		return nil
	})
	return found
}

func TestIsAPIPath(t *testing.T) {
	cases := map[string]bool{
		"/api/v1/tasks":  true,
		"/api/v1/health": true,
		"/mcp":           true,
		"/mcp/":          true,
		"/.well-known/oauth-authorization-server": true,
		"/authorize":            true,
		"/token":                true,
		"/register":             true,
		"/health":               true,
		"/":                     false,
		"/tasks":                false,
		"/auth/callback":        false,
		"/assets/index-abc.js":  false,
		"/manifest.webmanifest": false,
		// must not false-positive on names that merely contain a prefix
		"/authorized-users": false,
		"/tokens-page":      false,
	}
	for path, want := range cases {
		if got := isAPIPath(path); got != want {
			t.Errorf("isAPIPath(%q) = %v, want %v", path, got, want)
		}
	}
}

// apiStub records whether it was called and with what method/path.
type apiStub struct {
	called bool
	method string
	path   string
}

func (s *apiStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.called = true
	s.method = r.Method
	s.path = r.URL.Path
	w.WriteHeader(http.StatusTeapot) // distinctive marker
}

func TestHandler_DelegatesAPIPaths(t *testing.T) {
	// Every sensitive prefix must be delegated to the API even as a plain GET —
	// never served the SPA shell (index.html). This is the guard that prevents a
	// misrouted request (or a stale service worker doing a navigation-style GET
	// to an API path) from receiving HTML instead of JSON. Includes unknown
	// routes, which must still 404 as JSON from the API rather than fall through
	// to the SPA.
	paths := []string{
		"/api/v1/tasks",
		"/api/v1/entries",
		"/api/v1/this-route-does-not-exist",
		"/mcp",
		"/authorize",
		"/token",
		"/register",
		"/health",
		"/.well-known/oauth-authorization-server",
		"/oauth/anything",
	}
	for _, p := range paths {
		stub := &apiStub{}
		h := Handler(stub)
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if !stub.called {
			t.Errorf("GET %q: expected delegation to API, but the SPA handled it (would serve index.html)", p)
		}
		if rec.Code != http.StatusTeapot {
			t.Errorf("GET %q: expected 418 from API stub, got %d", p, rec.Code)
		}
	}
}

func TestHandler_DelegatesNonGETRoot(t *testing.T) {
	// POST / must reach the API handler (MCP-at-root), not the SPA.
	stub := &apiStub{}
	h := Handler(stub)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !stub.called {
		t.Fatal("expected API stub to be called for POST /")
	}
}

func TestHandler_ServesSPAForBrowserRoutes(t *testing.T) {
	if !IsBuilt() {
		t.Skip("SPA not built into this binary; run `just web-build` before testing")
	}
	stub := &apiStub{}
	h := Handler(stub)

	for _, path := range []string{"/", "/runners", "/auth/callback"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if stub.called {
			t.Fatalf("API stub should not be called for SPA route %q", path)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("GET %q: expected 200, got %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("GET %q: expected html content type, got %q", path, ct)
		}
	}
}

func TestHandler_HashedAssetsAreImmutable(t *testing.T) {
	if !IsBuilt() {
		t.Skip("SPA not built into this binary")
	}
	sub := mustSub(t)
	// Find a built asset under assets/.
	asset := findAsset(t, sub)
	if asset == "" {
		t.Skip("no built assets found")
	}

	h := Handler(&apiStub{})
	req := httptest.NewRequest(http.MethodGet, "/"+asset, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s: expected 200, got %d", asset, rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("expected immutable cache header for %s, got %q", asset, cc)
	}
}
