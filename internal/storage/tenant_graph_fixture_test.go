package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Test-only subprocess seam: apiserver cannot import storage's export_test.go.
// The parent opens a fresh v28 owner BEFORE staging, just like migratedNoteStores.
// No production constructor accepts v29 and no production migration API is added.
func TestTenantGraphFixtureStage(t *testing.T) {
	path := os.Getenv("BRAIN_GRAPH_FIXTURE")
	if path == "" {
		t.Skip("apiserver fixture child only")
	}
	if filepath.Base(path) != "graph-fixture.db" || !strings.Contains(filepath.Dir(path), "Test") && !strings.Contains(filepath.Dir(path), "Benchmark") {
		t.Fatal("not a test-allocated fixture path")
	}
	n, err := strconv.Atoi(os.Getenv("BRAIN_GRAPH_TENANTS"))
	if err != nil || n < 1 || n > 500 {
		t.Fatal("invalid fixture size")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var count int
	if err := db.QueryRow("SELECT count(*) FROM notes").Scan(&count); err != nil || count != 0 {
		t.Fatal("fixture must be empty", err)
	}
	if v, err := GetSchemaVersion(db); err != nil || v != 30 || CurrentSchemaVersion != 30 {
		t.Fatal("fixture must be genuine runtime30", err)
	}
	if err := migrateSuccessorSchema(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	relationalExec(t, tx, "DROP TRIGGER p4_fts_map_insert")
	for i := 1; i < n; i++ {
		id, indexID := fmt.Sprintf("tenant-%03d", i), fmt.Sprintf("%032x", i)
		if _, err := tx.Exec("INSERT INTO tenants(id,name,status,created_at) VALUES(?,?,'active','now')", id, id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("INSERT INTO entry_sync_identity VALUES(?,1,lower(hex(randomblob(16))))", id); err != nil {
			t.Fatal(err)
		}
		name, err := tenantFTSName(indexID)
		if err != nil {
			t.Fatal(err)
		}
		relationalExec(t, tx, tenantFTSTableDDL(name))
		if _, err := tx.Exec("INSERT INTO tenant_fts(tenant_id,internal_id) VALUES(?,?)", id, indexID); err != nil {
			t.Fatal(err)
		}
	}
	mapping, err := readTenantFTSMapping(tx)
	if err != nil {
		t.Fatal(err)
	}
	for name, ddl := range tenantFTSTriggers(mapping) {
		relationalExec(t, tx, "DROP TRIGGER IF EXISTS "+name)
		relationalExec(t, tx, ddl)
	}
	if _, err := checkTenantSearchCatalogComplete(tx, true); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
