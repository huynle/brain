package storage_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

type maintenanceEmbedder struct{}

func (maintenanceEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	vectors := make([][]float32, len(inputs))
	for i := range inputs {
		vectors[i] = []float32{1, 2, 3}
	}
	return vectors, nil
}

func writeMaintenanceNote(t *testing.T, root, path, body string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("---\ntitle: "+body+"\ntype: note\n---\n\n"+body+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func embedMaintenanceNotes(t *testing.T, idx *indexer.Indexer) {
	t.Helper()
	r, err := idx.IndexEmbeddingsWithOptions(context.Background(), maintenanceEmbedder{}, indexer.EmbeddingIndexOptions{Force: true})
	if err != nil || r.Failed != 0 || r.Processed == 0 {
		t.Fatalf("embed: %+v, %v", r, err)
	}
}

func TestMigratedIndexerMaintenanceIsolation(t *testing.T) {
	for _, operation := range []string{"rebuild", "incremental", "health", "empty"} {
		t.Run(operation, func(t *testing.T) {
			db, a, b := storage.MigratedNoteStoresForTest(t)
			// The migration fixture has legacy local data; clear only that fixture
			// data before establishing the two independent filesystem roots.
			if _, err := db.Exec("DELETE FROM notes WHERE tenant_id='local'"); err != nil {
				t.Fatal(err)
			}
			rootA, rootB := t.TempDir(), t.TempDir()
			idxA, idxB := indexer.NewIndexer(rootA, a), indexer.NewIndexer(rootB, b)
			paths := []string{"projects/shared/note/same.md", "global/note/global.md", "projects/shared/note/null.md", "projects/shared/note/deleted.md"}
			for _, path := range paths {
				writeMaintenanceNote(t, rootA, path, "A original "+path)
				writeMaintenanceNote(t, rootB, path, "B distinct "+path)
				if err := idxA.IndexFile(path); err != nil {
					t.Fatal(err)
				}
				if err := idxB.IndexFile(path); err != nil {
					t.Fatal(err)
				}
			}
			const onlyB = "projects/shared/note/only-b.md"
			writeMaintenanceNote(t, rootB, onlyB, "B only")
			if err := idxB.IndexFile(onlyB); err != nil {
				t.Fatal(err)
			}
			embedMaintenanceNotes(t, idxB)
			// Use a sentinel rather than a sleep: embedding timestamps have
			// second precision, so an accidental same-second stamp can look intact.
			if _, err := db.Exec("UPDATE note_embeddings_meta SET embedding_indexed_at='2000-01-01 00:00:00' WHERE tenant_id=?", b.TenantID().String()); err != nil {
				t.Fatal(err)
			}
			before := storage.TenantIndexSnapshotForTest(t, b)
			for table, rows := range before {
				if len(rows) != 5 {
					t.Fatalf("B %s fixture has %d rows; want 5", table, len(rows))
				}
			}
			defer func() {
				if after := storage.TenantIndexSnapshotForTest(t, b); !reflect.DeepEqual(before, after) {
					t.Errorf("B full notes/embedding snapshot changed after A %s", operation)
				}
			}()
			switch operation {
			case "rebuild":
				r, err := idxA.RebuildAll()
				if err != nil || r.Added != 4 || r.Deleted != 4 || len(r.Errors) != 0 {
					t.Fatalf("A rebuild: %+v, %v; want added=4 deleted=4", r, err)
				}
				embedMaintenanceNotes(t, idxA)
			case "incremental":
				writeMaintenanceNote(t, rootA, paths[0], "A modified")
				if _, err := a.UpdateNote(context.Background(), paths[2], map[string]interface{}{"checksum": nil}); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(rootA, paths[3])); err != nil {
					t.Fatal(err)
				}
				writeMaintenanceNote(t, rootA, onlyB, "A newly added despite B existing")
				r, err := idxA.IndexChanged()
				if err != nil || r.Added != 1 || r.Updated != 2 || r.Deleted != 1 || r.Skipped != 1 || len(r.Errors) != 0 {
					t.Fatalf("A incremental: %+v, %v; want 1/2/1/1", r, err)
				}
				r, err = idxA.IndexChanged()
				if err != nil || r.Added != 0 || r.Updated != 0 || r.Deleted != 0 || r.Skipped != 4 || len(r.Errors) != 0 {
					t.Fatalf("repeat scan: %+v, %v", r, err)
				}
				embedMaintenanceNotes(t, idxA)
			case "health":
				if err := os.Remove(filepath.Join(rootA, paths[3])); err != nil {
					t.Fatal(err)
				}
				h, err := idxA.GetHealth()
				if err != nil || h.TotalFiles != 3 || h.TotalIndexed != 4 || h.StaleCount != 1 {
					t.Fatalf("A health: %+v, %v; want 3/4/1", h, err)
				}
			case "empty":
				idxA = indexer.NewIndexer(t.TempDir(), a)
				r, err := idxA.IndexChanged()
				if err != nil || r.Deleted != 4 || r.Added != 0 {
					t.Fatalf("empty scan: %+v, %v", r, err)
				}
				r, err = idxA.RebuildAll()
				if err != nil || r.Deleted != 0 || r.Added != 0 {
					t.Fatalf("empty rebuild: %+v, %v", r, err)
				}
				h, err := idxA.GetHealth()
				if err != nil || h.TotalFiles != 0 || h.TotalIndexed != 0 || h.StaleCount != 0 {
					t.Fatalf("empty health: %+v, %v", h, err)
				}
			}
		})
	}
}

func TestIndexerMaintenanceV28PathsAndGuards(t *testing.T) {
	if storage.CurrentSchemaVersion != 28 {
		t.Fatal("runtime schema activated")
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	owner, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatal(err)
	}
	local, err := owner.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	paths := []string{"projects/demo/note/project.md", "global/note/global.md"}
	idx := indexer.NewIndexer(root, local)
	for _, path := range paths {
		writeMaintenanceNote(t, root, path, path)
	}
	for _, scan := range []func() (*indexer.IndexResult, error){idx.IndexChanged, idx.RebuildAll} {
		r, err := scan()
		if err != nil || r.Added != 2 || len(r.Errors) != 0 {
			t.Fatalf("local scan: %+v, %v", r, err)
		}
		for _, path := range paths {
			n, err := local.GetNoteByPath(context.Background(), path)
			if err != nil || n == nil || n.Path != path {
				t.Fatalf("exact path %q: %+v, %v", path, n, err)
			}
		}
	}
	foreign, err := owner.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		store   *storage.TenantStore
		version int
		want    string
	}{
		{"nonlocal-v28", foreign, 28, "v28 content requires local tenant"},
		{"unsupported", local, 30, "unsupported tenant content schema 30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Empty disk means no subsequent InsertNote can accidentally supply
			// the guard after a global destructive delete has already happened.
			guarded := indexer.NewIndexer(t.TempDir(), tc.store)
			for _, op := range []struct {
				name string
				run  func() error
			}{
				{"rebuild", func() error { _, err := guarded.RebuildAll(); return err }},
				{"incremental", func() error { _, err := guarded.IndexChanged(); return err }},
				{"health", func() error { _, err := guarded.GetHealth(); return err }},
			} {
				if _, err := db.Exec("UPDATE schema_version SET version=28"); err != nil {
					t.Fatal(err)
				}
				if _, err := idx.RebuildAll(); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("UPDATE schema_version SET version=?", tc.version); err != nil {
					t.Fatal(err)
				}
				if err := op.run(); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("%s guard: %v; want %s", op.name, err, tc.want)
				}
				var count int
				if err := db.QueryRow("SELECT count(*) FROM notes").Scan(&count); err != nil || count != 2 {
					t.Errorf("%s destroyed local data: count=%d, %v", op.name, count, err)
				}
			}
		})
	}
}
