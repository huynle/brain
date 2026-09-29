package apiserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

type testHTTPGraph struct {
	countedGraph
	serve func(http.ResponseWriter, *http.Request)
}

func (g *testHTTPGraph) ServeHTTP(w http.ResponseWriter, r *http.Request) { g.serve(w, r) }

func TestTenantHTTPSelectionAndHeldRequest(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	var builds atomic.Int32
	g := &testHTTPGraph{serve: func(w http.ResponseWriter, r *http.Request) {
		id, ok := tenant.From(r.Context())
		if !ok || id != tenant.Local {
			t.Error("tenant context lost")
		}
		close(entered)
		<-r.Context().Done()
		<-finish
		w.WriteHeader(204)
	}}
	m, _ := newTenantGraphManager(1, activeGraphAuthority, func(context.Context, tenant.ID) (graphResource, error) { builds.Add(1); return g, nil })
	h := tenantWorkloadHTTP(m)
	// A header is not authority, even if it names a valid tenant.
	for _, header := range []string{"", "local"} {
		r := httptest.NewRequest("GET", "/api/v1/entries/", nil)
		r.Header.Set("X-Brain-Tenant", header)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("missing trusted tenant: %d", w.Code)
		}
	}
	if builds.Load() != 0 {
		t.Fatal("untrusted request built graph")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := httptest.NewRequest("GET", "/api/v1/entries/", nil)
		r = r.WithContext(tenant.Into(r.Context(), tenant.Local))
		h.ServeHTTP(httptest.NewRecorder(), r)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler not reached")
	}
	m.Invalidate(tenant.Local)
	if g.closes.Load() != 0 {
		t.Fatal("closed during request")
	}
	close(finish)
	<-done
	shutdownGraphs(t, m)
	if builds.Load() != 1 || g.closes.Load() != 1 {
		t.Fatal("request did not retain exactly one graph lease")
	}
}

func TestTenantHTTPRealFactoryAndUnsupportedSurfaces(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	if _, err := views.roots.ProvisionLocal(ctx, root, filepath.Join(root, "attachments")); err != nil {
		t.Fatal(err)
	}
	g, err := newTenantGraph(ctx, views.tenant, views.roots, config.Config{}, graphIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	e, err := g.brain.Save(ctx, types.CreateEntryRequest{Type: "note", Title: "Staged HTTP content", Content: "local content", Project: "demo"})
	g.Close()
	if err != nil {
		t.Fatal(err)
	}
	factory := tenantHTTPFactory(func(id tenant.ID) (*storage.TenantStore, error) { return views.tenant, nil }, views.roots, config.Config{})
	if factory == nil {
		t.Fatal("missing real handler factory seam")
	}
	m, err := newTenantGraphManager(2, activeGraphAuthority, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdownGraphs(t, m)
	h := tenantWorkloadHTTP(m)
	for _, path := range []string{"/api/v1/entries/", "/api/v1/entries/" + e.Path, "/api/v1/stats"} {
		r := httptest.NewRequest("GET", path, nil)
		r = r.WithContext(tenant.Into(r.Context(), tenant.Local))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/tasks/", "/api/v1/control/runners/x/sessions/y/history", "/api/v1/runners/", "/api/v1/instances", "/api/v1/config", "/api/v1/tokens/", "/api/v1/auth/login", "/api/v1/events/stream", "/mcp", "/api/v1/assistant/status", "/api/v1/health"} {
		for _, method := range []string{"GET", "POST"} {
			r := httptest.NewRequest(method, path, nil)
			r = r.WithContext(tenant.Into(r.Context(), tenant.Local))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 501 {
				t.Fatalf("unsupported %s %s exposed: %d", method, path, w.Code)
			}
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/entries/", nil)
	r = r.WithContext(tenant.Into(r.Context(), tenant.Local))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 501 {
		t.Fatalf("unreviewed mutation exposed: %d", w.Code)
	}
	// A trusted factory may not accidentally bind another tenant's handle.
	if _, err := m.Acquire(ctx, tenant.MustParse("foreign")); err == nil {
		t.Fatal("factory accepted mismatched store")
	}
}
