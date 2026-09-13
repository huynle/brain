package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBulkJobsMigrationFrom28(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brain.db")
	db := archivedSchemaFixture(t, provenanceSources[0].revision)
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	// Exercise the migration directly, without InitSchema creating tables first.
	for _, table := range []string{"bulk_jobs", "bulk_job_items"} {
		var n int
		if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	// Ordinary admission uses a separate genuine source, never the unversioned
	// intermediate output of the private migration-body unit test above.
	runArchivedSchema(t, provenanceSources[0].revision, path)
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if version, err := GetSchemaVersion(s.db); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("version %d: %v", version, err)
	}
}
