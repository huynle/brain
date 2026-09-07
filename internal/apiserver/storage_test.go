package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestStorageViewsShareDatabaseAndBoundLocalIdentity(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	v, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(dir, "brain.db"), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer v.close()
	if v.tenant.TenantID() != tenant.Local {
		t.Fatal("data view is not local")
	}
	if _, err := v.roots.ProvisionLocal(ctx, dir, filepath.Join(dir, "attachments")); err != nil {
		t.Fatal(err)
	}
	var roots int
	if err := v.tenant.DB().QueryRow("SELECT count(*) FROM tenant_roots").Scan(&roots); err != nil || roots != 1 {
		t.Fatalf("registry and workload do not share database: count=%d err=%v", roots, err)
	}
	if err := v.tokens.BootstrapToken(ctx, "first", "first-secret", false); err != nil {
		t.Fatal(err)
	}
	if token, err := v.identity.ValidateToken(ctx, "first-secret"); err != nil || token.Scope != "admin:*" {
		t.Fatalf("identity does not share token backing: %v %v", token, err)
	}
	// No global adapter/capability was delegated to this identity view.
	if _, err := v.identity.TenantRegistry(auth.DeploymentOperator{}); err == nil {
		t.Fatal("identity view bypassed operator check")
	}
	pool := v.tenant.DB()
	v.close()
	if err := pool.Ping(); err == nil {
		t.Fatal("owner cleanup did not close shared pool")
	}
}

func TestStorageCompositionRejectsMultiBeforeOpeningDatabase(t *testing.T) {
	for _, mode := range []tenant.Mode{tenant.ModeMulti, "bad-mode"} {
		path := filepath.Join(t.TempDir(), "unopened.db")
		v, err := openSingleModeStorage(context.Background(), mode, path, t.TempDir(), false)
		if err == nil || v != nil {
			t.Fatalf("mode=%s returned views=%v err=%v", mode, v, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("rejected mode opened database: %v", err)
		}
	}
}

func TestSingleModeHTTPTokenCompatibility(t *testing.T) {
	t.Setenv(auth.EnvPasswordHash, "")
	t.Setenv("BRAIN_ALLOW_REMOTE_BOOTSTRAP", "")
	ctx, cancel := context.WithCancel(context.Background())
	h, _, cleanup, err := buildHTTPHandler(ctx, ServerOptions{Host: "localhost", BrainDir: t.TempDir(), EnableAuth: true})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); cleanup() }()
	request := func(method, path, peer, bearer, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = peer
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/api/v1/tokens/bootstrap", "192.0.2.1:1234", "", `{"name":"remote"}`); w.Code != http.StatusForbidden {
		t.Fatalf("remote bootstrap=%d %s", w.Code, w.Body.String())
	}
	w := request("POST", "/api/v1/tokens/bootstrap", "127.0.0.1:1234", "", `{"name":"first"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap=%d %s", w.Code, w.Body.String())
	}
	var first struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/v1/tokens", "127.0.0.1:1234", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous enumeration=%d", w.Code)
	}
	if w := request("GET", "/api/v1/tokens", "127.0.0.1:1234", first.Token, ""); w.Code != http.StatusOK {
		t.Fatalf("admin compatibility=%d %s", w.Code, w.Body.String())
	}
	w = request("POST", "/api/v1/tokens", "127.0.0.1:1234", first.Token, `{"name":"reader","scope":"read:*"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create reader=%d %s", w.Code, w.Body.String())
	}
	var reader struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reader); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/v1/tokens", "127.0.0.1:1234", reader.Token, ""); w.Code != http.StatusForbidden {
		t.Fatalf("read-scope enumeration=%d", w.Code)
	}
	// Data-plane routing is wired to the local handle, not to identity/control.
	if w := request("GET", "/api/v1/tasks", "127.0.0.1:1234", first.Token, ""); w.Code != http.StatusOK {
		t.Fatalf("workload routing=%d %s", w.Code, w.Body.String())
	}
	if w := request("DELETE", "/api/v1/tokens/reader", "127.0.0.1:1234", first.Token, ""); w.Code != http.StatusOK {
		t.Fatalf("revoke reader=%d %s", w.Code, w.Body.String())
	}
	if w := request("DELETE", "/api/v1/tokens/first", "127.0.0.1:1234", first.Token, ""); w.Code != http.StatusOK {
		t.Fatalf("revoke first=%d %s", w.Code, w.Body.String())
	}
	if w := request("POST", "/api/v1/tokens/bootstrap", "127.0.0.1:1234", "", `{"name":"reopen"}`); w.Code != http.StatusForbidden {
		t.Fatalf("bootstrap reopened after revocation=%d %s", w.Code, w.Body.String())
	}
}
