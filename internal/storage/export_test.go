package storage

import (
	"database/sql"
	"sort"
	"testing"
)

// MigratedNoteStoresForTest exposes only the real migration fixture to external
// indexer integration tests, without adding a production migration entry point.
// The newly created fixture owns cleanup of the returned connection.
func MigratedNoteStoresForTest(t *testing.T) (*sql.DB, *TenantStore, *TenantStore) {
	owner, a, b := migratedNoteStores(t)
	return owner.db, a, b
}

func TenantIndexSnapshotForTest(t *testing.T, s *TenantStore) map[string][]string {
	t.Helper()
	result := map[string][]string{}
	for _, table := range []string{"notes", "note_embeddings_meta", "note_embeddings"} {
		result[table] = relationalSnapshot(t, s.db, "(SELECT * FROM "+table+" WHERE tenant_id='"+s.TenantID().String()+"')")
		sort.Strings(result[table])
	}
	return result
}
