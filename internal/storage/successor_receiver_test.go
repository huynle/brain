package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

func TestReceiverReadinessIsTenantLocal(t *testing.T) {
	for _, damage := range []string{"missing-cas", "corrupt-cas", "fts-segment", "missing-epoch", "foreign-key"} {
		t.Run(damage, func(t *testing.T) {
			db := successorPrivateFixture(t)
			resolver, err := tenantfs.New(registryHandle(t, &StorageLayer{db: db}), t.TempDir())
			collisionMust(t, err)
			data := []byte("receiver-owned CAS")
			digest := fmt.Sprintf("%x", sha256.Sum256(data))
			paths := map[string]string{}
			for _, id := range []string{"a", "b"} {
				p, err := resolver.BlobPath(context.Background(), tenant.MustParse(id), digest)
				collisionMust(t, err)
				collisionMust(t, os.MkdirAll(filepath.Dir(p), 0700))
				collisionMust(t, os.WriteFile(p, data, 0600))
				paths[id] = p
				_, err = db.Exec("INSERT INTO attachments(tenant_id,digest,size) VALUES(?,?,?)", id, digest, len(data))
				collisionMust(t, err)
			}
			switch damage {
			case "missing-cas":
				collisionMust(t, os.Remove(paths["a"]))
			case "corrupt-cas":
				collisionMust(t, os.WriteFile(paths["a"], []byte("corrupt"), 0600))
			case "fts-segment":
				var internal string
				collisionMust(t, db.QueryRow("SELECT internal_id FROM tenant_fts WHERE tenant_id='a'").Scan(&internal))
				relationalExec(t, db, "DELETE FROM fts_t_"+internal+"_data WHERE id>10")
			case "missing-epoch":
				relationalExec(t, db, "DELETE FROM entry_sync_identity WHERE tenant_id='a'")
			case "foreign-key":
				relationalExec(t, db, "PRAGMA foreign_keys=OFF; INSERT INTO tags(tenant_id,note_id,tag) VALUES('a',999999,'dangling'); PRAGMA foreign_keys=ON")
			}
			before := recoverySnapshot(t, db)
			for _, id := range []string{"b", "a"} {
				s := &TenantStore{db: db, tenantID: tenant.MustParse(id)}
				ctx := tenant.Into(context.Background(), s.TenantID())
				ops := map[string]func() error{
					"content": func() error { _, e := s.GetNoteByPath(ctx, "same"); return e },
					"search":  func() error { _, e := s.SearchNotes(ctx, "alpha", nil); return e },
					"sync":    func() error { _, e := s.ReadEntryChanges(ctx, "", 0, 10); return e },
					"budget":  func() error { _, _, _, e := s.ExecutionBudget(ctx, "p", "missing", time.Now()); return e },
				}
				for name, run := range ops {
					err := run()
					if id == "b" && err != nil {
						t.Errorf("healthy B %s denied by A damage: %v", name, err)
					}
					if id == "a" && err == nil {
						t.Errorf("damaged A %s admitted", name)
					}
				}
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("ordinary readiness mutated database/foreign FTS")
			}
			if err := migrateSuccessorSchema(context.Background(), db, nil); err == nil {
				t.Fatal("global reopen validation admitted damage")
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("global refusal repaired damage")
			}
		})
	}
}
