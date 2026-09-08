package apiserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestPreChangeTokenResolvesLocalSearchAfterReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brain.db")
	old, err := storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "fixture-existing-single-mode-token"
	if _, err := old.DB().Exec("INSERT INTO api_tokens(name,token,scope) VALUES('existing',?,'read:*')", secret); err != nil {
		t.Fatal(err)
	}
	if _, err := old.DB().Exec("INSERT INTO notes(path,short_id,title) VALUES('projects/p/note/same.md','same0001','legacyneedle')"); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	v, err := openSingleModeStorage(context.Background(), tenant.ModeSingle, path, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer v.close()
	called := false
	h := api.Auth(true, v.identity)(api.TenantScope(tenant.ModeSingle, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		id, ok := tenant.From(r.Context())
		if !ok || id != tenant.Local || v.tenant.TenantID() != id {
			t.Fatal("authenticated request did not resolve local")
		}
		rows, err := v.tenant.SearchNotes(r.Context(), "legacyneedle", nil)
		if err != nil || len(rows) != 1 || rows[0].ShortID != "same0001" {
			t.Fatalf("legacy search: %v %v", rows, err)
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called || w.Code != http.StatusNoContent {
		t.Fatalf("legacy auth status=%d", w.Code)
	}
	if version, err := storage.GetSchemaVersion(v.tenant.DB()); err != nil || version != 28 {
		t.Fatalf("runtime version=%d %v", version, err)
	}
}
