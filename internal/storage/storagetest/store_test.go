package storagetest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
)

func TestPathFixtureDrainsBeforeOwnerClose(t *testing.T) {
	var view *storage.TenantStore
	drained := false
	t.Run("owned lifetime", func(t *testing.T) {
		var err error
		view, err = New(t, filepath.Join(t.TempDir(), "brain.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := view.ListNotes(context.Background(), nil); err != nil {
				t.Errorf("database closed before worker drain: %v", err)
			}
			drained = true
		})
	})
	if !drained {
		t.Fatal("worker cleanup not executed")
	}
	if _, err := view.ListNotes(context.Background(), nil); err == nil {
		t.Fatal("path fixture leaked database after owner cleanup")
	}
}

func TestWithDBFixtureLeavesLifetimeWithCaller(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var view *storage.TenantStore
	t.Run("borrow", func(t *testing.T) {
		view, err = NewWithDB(db)
		if err != nil {
			t.Fatal(err)
		}
	})
	if _, err := view.ListNotes(context.Background(), nil); err != nil {
		t.Fatalf("borrower closed caller DB: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := view.ListNotes(context.Background(), nil); err == nil {
		t.Fatal("view ignored explicit caller close")
	}
}
