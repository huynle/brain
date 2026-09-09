package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBulkJobsMigrationFrom28(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brain.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec("DROP TABLE bulk_job_items; DROP TABLE bulk_jobs; DELETE FROM schema_version; INSERT INTO schema_version(version) VALUES(28)"); err != nil {
		t.Fatal(err)
	}
	if err = migrateSchema(s.DB()); err != nil {
		t.Fatal(err)
	}
	// Exercise the migration directly, without InitSchema creating tables first.
	for _, table := range []string{"bulk_jobs", "bulk_job_items"} {
		var n int
		if err = s.DB().QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if version, err := GetSchemaVersion(s.DB()); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("version %d: %v", version, err)
	}
}
