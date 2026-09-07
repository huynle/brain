package storage

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestTenantRelationalRehearsalChild(t *testing.T) {
	path := os.Getenv("BRAIN_P4_REHEARSAL_CHILD")
	if path == "" {
		t.Skip("parent-only subprocess harness")
	}
	// This entry point is test-only. No application constructor or worker starts.
	db := compatibilityDB(t, path)
	db.SetMaxOpenConns(1)
	relationalExec(t, db, "PRAGMA wal_autocheckpoint=0")
	// Force dirty-page spill so SIGKILL exercises an uncommitted WAL, not just RAM.
	relationalExec(t, db, "PRAGMA cache_size=10; PRAGMA cache_spill=ON")
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow("SELECT count(*) FROM tenants WHERE id='local'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("staging not observed: %d %v", n, err)
	}
	fmt.Println("P4_STAGED_UNCOMMITTED")
	// Parent kills this process only after the readiness handshake. A bounded
	// fallback exits without committing if the parent disappears.
	time.Sleep(55 * time.Second)
	t.Fatal("parent did not interrupt child")
}
