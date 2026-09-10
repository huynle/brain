package storage

import (
	"context"
	"github.com/huynle/brain-api/internal/tenant"
	"path/filepath"
	"testing"
)

func TestSyncDurableReceiptsAndTransactionalFeed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "brain.db")
	base, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	scoped, _ := base.ForTenant(tenant.Local)
	first, err := scoped.ReadEntryChanges(ctx, "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := base.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO notes(path,short_id,title) VALUES ('projects/p/scratch/a.md','a','temporary')`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	next, err := scoped.ReadEntryChanges(ctx, first.Epoch, first.Cursor, 10)
	if err != nil || len(next.Rows) != 0 {
		t.Fatal("rolled back changes escaped", next, err)
	}
	if r, err := scoped.ReserveSyncOperation(ctx, "done", "hash"); err != nil || r != nil {
		t.Fatal(r, err)
	}
	if err = scoped.CompleteSyncOperation(ctx, "done", 201, `{"path":"created"}`); err != nil {
		t.Fatal(err)
	}
	if _, err = scoped.ReserveSyncOperation(ctx, "interrupted", "hash"); err != nil {
		t.Fatal(err)
	}
	if err = base.Close(); err != nil {
		t.Fatal(err)
	}
	base, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	scoped, _ = base.ForTenant(tenant.Local)
	if r, err := scoped.ReserveSyncOperation(ctx, "done", "hash"); err != nil || r == nil || r.Status != 201 {
		t.Fatal("receipt did not survive reopen", r, err)
	}
	if r, err := scoped.ReserveSyncOperation(ctx, "interrupted", "hash"); err != nil || r == nil || r.Status != 0 {
		t.Fatal("interrupted write allowed to replay", r, err)
	}
	if _, err = scoped.ReserveSyncOperation(ctx, "done", "different hash"); err == nil {
		t.Fatal("same id accepted different content")
	}
	other, _ := base.ForTenant(tenant.MustParse("other"))
	if _, err = other.ReadEntryChanges(ctx, "", 0, 10); err == nil {
		t.Fatal("non-local sync scope admitted")
	}
	next, err = scoped.ReadEntryChanges(ctx, first.Epoch, 0, 10)
	if err != nil || next.Epoch != first.Epoch {
		t.Fatal("epoch changed on ordinary restart", err)
	}
}
