package apiserver

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestRunServerRejectsOperationalMultiBeforeStorage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("BRAIN_TENANT_MODE", "multi")
	dir := filepath.Join(t.TempDir(), "must-not-exist")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunServer(ctx, ServerOptions{BrainDir: dir, Host: "127.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "multi") {
		t.Errorf("startup error = %v; want operational multi guard", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("multi startup touched storage: %v", err)
	}
}

func TestBackgroundTenantContext(t *testing.T) {
	ctx, err := backgroundTenantContext(context.Background(), tenant.ModeSingle)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := tenant.From(ctx); !ok || id != tenant.Local {
		t.Errorf("bootstrap tenant = %v, %v", id, ok)
	}
	for _, mode := range []tenant.Mode{tenant.ModeMulti, "bad"} {
		if _, err := backgroundTenantContext(context.Background(), mode); err == nil {
			t.Errorf("accepted mode %q", mode)
		}
	}
	if _, err := backgroundTenantContext(tenant.Into(context.Background(), tenant.MustParse("foreign")), tenant.ModeSingle); err == nil {
		t.Error("accepted foreign inherited bootstrap scope")
	}
}

func TestEmbeddedMCPRejectsForeignInheritedTenant(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, _, cleanup, err := buildHTTPHandler(ctx, ServerOptions{Host: "127.0.0.1", BrainDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, path := range []string{"/mcp", "/"} {
		for _, method := range []string{"POST", "DELETE"} {
			r := httptest.NewRequest(method, path+"?tenant=foreign", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"tenant":"foreign"}}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Tenant-ID", "foreign")
			r = r.WithContext(tenant.Into(r.Context(), tenant.MustParse("foreign")))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Errorf("%s %s = %d; want tenant rejection", method, path, w.Code)
			}
		}
	}
}
