package apiserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestStartupPersistsLocalFilesystemBeforeScan(t *testing.T) {
	dir := t.TempDir()
	blobs := t.TempDir()
	for _, name := range []string{"projects/p/note/local001.md", "tenants/foreign/note/foreign1.md"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\ntitle: Fixture\n---\nbody"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, cleanup, err := buildHTTPHandler(ctx, ServerOptions{Host: "127.0.0.1", BrainDir: dir, Attachments: config.AttachmentConfig{StorageRoot: blobs}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	db := filepath.Join(dir, config.DataDir, "brain.db")
	store, err := storage.New(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	maps, err := store.ListTenantRoots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 {
		t.Fatalf("startup did not persist local mapping: %v", maps)
	}
	if maps[0].ID != tenant.Local || maps[0].BrainRoot != dir || maps[0].BlobRoot != blobs {
		t.Fatalf("changed configured roots: %+v", maps[0])
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		row, err := store.GetNoteByPath(ctx, "projects/p/note/local001.md")
		if err != nil {
			t.Fatal(err)
		}
		if row != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("local boot scan never indexed note")
		}
		time.Sleep(10 * time.Millisecond)
	}
	row, err := store.GetNoteByPath(ctx, "tenants/foreign/note/foreign1.md")
	if err != nil {
		t.Fatal(err)
	}
	if row != nil {
		t.Fatal("boot scan ingested foreign note")
	}
}
