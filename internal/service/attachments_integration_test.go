package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/storage/storagetest"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
	"github.com/huynle/brain-api/internal/types"
)

func TestAttachmentServiceCompatibilityRejectsBeforeBlobIO(t *testing.T) {
	for _, mode := range []string{"nonlocal-v28", "zero-handle", "unsupported-schema", "closed-store"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			svc, _, blobs, db := newAttachmentServiceWithDBForTest(t, 1024)
			switch mode {
			case "nonlocal-v28":
				id, err := tenant.Parse("tenant-b")
				if err != nil {
					t.Fatal(err)
				}
				owner, err := storage.NewWithDB(db)
				if err != nil {
					t.Fatal(err)
				}
				svc.storage, err = owner.ForTenant(id)
				if err != nil {
					t.Fatal(err)
				}
			case "zero-handle":
				svc.storage = &storage.TenantStore{}
			case "unsupported-schema":
				if _, err := db.Exec("UPDATE schema_version SET version = 30"); err != nil {
					t.Fatal(err)
				}
			case "closed-store":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.Create(ctx, "proj", types.CreateAttachmentRequest{Filename: "x.txt", Size: 4}, strings.NewReader("data")); err == nil {
				t.Fatal("invalid compatibility scope accepted upload")
			}
			if blobs.putCalls != 0 || len(blobs.getCalls) != 0 || len(blobs.deleteCall) != 0 {
				t.Fatalf("denied upload touched CAS: puts=%d gets=%v deletes=%v", blobs.putCalls, blobs.getCalls, blobs.deleteCall)
			}
			if _, err := svc.List(ctx, "proj"); err == nil {
				t.Fatal("invalid compatibility scope accepted list")
			}
			if _, _, err := svc.Open(ctx, "proj", "1"); err == nil {
				t.Fatal("invalid compatibility scope accepted open")
			}
			if _, err := svc.Delete(ctx, "proj", "1"); err == nil {
				t.Fatal("invalid compatibility scope accepted delete")
			}
			if len(blobs.getCalls) != 0 || len(blobs.deleteCall) != 0 {
				t.Fatal("denied read/delete touched CAS")
			}
		})
	}
}

// Real BrainService, indexer, SQLite and mapped legacy CAS. This proves local
// cross-project references, not multi-tenant physical/async lifecycle isolation.
func TestAttachmentServiceLocalCrossProjectLifecycle(t *testing.T) {
	ctx := context.Background()
	brain, store, root, db := newTestBrainServiceWithDB(t)
	roots, err := tenantfs.New(storagetest.RegistryWithDB(t, db), root)
	if err != nil {
		t.Fatal(err)
	}
	casRoot := t.TempDir()
	if _, err := roots.ProvisionLocal(ctx, root, casRoot); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.NewTenantFilesystemStore(roots, tenant.Local, 1024)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAttachmentService(store, blobs, brain, 1024)
	a := createAttachmentForServiceTest(t, svc, "alpha", "first.txt")
	b := createAttachmentForServiceTest(t, svc, "beta", "second.txt")
	if a.ID != b.ID {
		t.Fatalf("equal bytes allocated different tenant IDs: %s / %s", a.ID, b.ID)
	}
	legacyPath := filepath.Join(casRoot, a.SHA256[:2], a.SHA256[2:4], a.SHA256)
	assertBytes := func() {
		t.Helper()
		data, err := os.ReadFile(legacyPath)
		if err != nil || string(data) != "data" {
			t.Fatalf("legacy shards changed: %q, %v", data, err)
		}
	}
	assertBytes()
	paths := map[string]string{}
	for _, project := range []string{"alpha", "beta"} {
		saved, err := brain.Save(ctx, types.CreateEntryRequest{Project: project, Type: "report", Title: "Attachment reference", Content: "body"})
		if err != nil {
			t.Fatal(err)
		}
		paths[project] = saved.Path
		if _, err := svc.Attach(ctx, project, saved.Path, types.AttachEntryAttachmentRequest{Attachment: types.AttachmentReference{ID: a.ID, Role: "source"}}); err != nil {
			t.Fatalf("Attach(%s): %v", project, err)
		}
		refs, err := svc.ListForEntry(ctx, project, saved.Path)
		if err != nil || len(refs.Attachments) != 1 || refs.Attachments[0].ID != a.ID {
			t.Fatalf("ListForEntry(%s): %+v, %v", project, refs, err)
		}
		listed, err := svc.List(ctx, project)
		if err != nil || listed.Total != 1 {
			t.Fatalf("List(%s): %+v, %v", project, listed, err)
		}
	}
	if _, err := svc.StoreDerivedText(ctx, "alpha", a.ID, types.AttachmentDerivedText{Status: "ready", Text: "shared derived"}); err != nil {
		t.Fatal(err)
	}
	_, text, err := svc.OpenText(ctx, "beta", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(text)
	_ = text.Close()
	if readErr != nil || string(data) != "shared derived" {
		t.Fatalf("same-tenant derived read: %q, %v", data, readErr)
	}
	if _, err := svc.Detach(ctx, "alpha", paths["alpha"], a.ID, "source"); err != nil {
		t.Fatal(err)
	}
	if deleted, err := svc.Delete(ctx, "alpha", a.ID); err != nil || deleted {
		t.Fatalf("beta still references bytes: deleted=%v err=%v", deleted, err)
	}
	assertBytes()
	count, err := store.CountAttachmentReferences(ctx, mustParseAttachmentIDForTest(t, a.ID))
	if err != nil || count != 1 {
		t.Fatalf("remaining tenant references=%d err=%v", count, err)
	}
	if _, err := svc.Detach(ctx, "beta", paths["beta"], a.ID, "source"); err != nil {
		t.Fatal(err)
	}
	if deleted, err := svc.Delete(ctx, "alpha", a.ID); err != nil || !deleted {
		t.Fatalf("unreferenced tenant attachment: deleted=%v err=%v", deleted, err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("unreferenced bytes remain: %v", err)
	}
	derived, err := store.GetAttachmentDerived(ctx, mustParseAttachmentIDForTest(t, a.ID), "text")
	if err != nil || derived != nil {
		t.Fatalf("derived row survived deletion: %+v, %v", derived, err)
	}
	// Already-deleted source cannot be recreated by an ordinary derived write.
	// This is not an epoch/generation check for an in-flight extractor.
	if _, err := svc.StoreDerivedText(ctx, "beta", a.ID, types.AttachmentDerivedText{Status: "ready", Text: "late"}); err == nil {
		t.Fatal("deleted source accepted derived text")
	}
	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil || version != 28 {
		t.Fatalf("runtime version changed: %d, %v", version, err)
	}
}
