package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/storage/storagetest"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

type failingRootsRepository struct {
	tenantfs.Repository
	calls int
}

var errRootsUnavailable = errors.New("root registry unavailable")

func (r *failingRootsRepository) ListTenantRoots(ctx context.Context) ([]tenantfs.Mapping, error) {
	r.calls++
	if r.calls > 1 {
		return nil, errRootsUnavailable
	}
	return r.Repository.ListTenantRoots(ctx)
}

func TestTenantScanPropagatesRegistryFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := storagetest.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := tenantfs.New(store, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ProvisionLocal(context.Background(), dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "projects"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "projects/local.md"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	failing, err := tenantfs.New(&failingRootsRepository{Repository: store}, dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = globMarkdownFiles(dir, failing.Brain(tenant.Local))
	if !errors.Is(err, errRootsUnavailable) {
		t.Fatalf("scan swallowed registry failure: %v", err)
	}
}

func TestWatcherStartPropagatesRegistryFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := storagetest.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := tenantfs.New(store, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ProvisionLocal(context.Background(), dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "projects"), 0700); err != nil {
		t.Fatal(err)
	}
	failing, err := tenantfs.New(&failingRootsRepository{Repository: store}, dir)
	if err != nil {
		t.Fatal(err)
	}
	fw, err := NewFileWatcher(dir, NewIndexer(dir, store, failing.Brain(tenant.Local)), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = fw.Start()
	if err == nil {
		fw.Stop()
	} else {
		fw.watcher.Close()
	}
	if !errors.Is(err, errRootsUnavailable) {
		t.Fatalf("watcher swallowed registry failure: %v", err)
	}
}

func TestWatcherContentRootRescanPropagatesRegistryFailure(t *testing.T) {
	dir := t.TempDir()
	store := newTestStorage(t)
	roots, err := tenantfs.New(store, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.ProvisionLocal(context.Background(), dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "projects"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "projects/first.md"), []byte(noteContent("First")), 0600); err != nil {
		t.Fatal(err)
	}
	// Root admission succeeds, but the relevant content root's lookup fails.
	failing, err := tenantfs.New(&failingRootsRepository{Repository: store}, dir)
	if err != nil {
		t.Fatal(err)
	}
	fw, err := NewFileWatcher(dir, NewIndexer(dir, store, failing.Brain(tenant.Local)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fw.rescanContentRoots(); !errors.Is(err, errRootsUnavailable) {
		t.Fatalf("recovery swallowed registry failure: %v", err)
	}
	if len(fw.pendingChanges) != 0 || countNotes(t, store) != 0 {
		t.Fatal("failed recovery queued or indexed content")
	}
}

func TestTenantPolicyScanAndWatcher(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storagetest.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := tenantfs.New(store, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ProvisionLocal(ctx, dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	idx := NewIndexer(dir, store, r.Brain(tenant.Local))
	write := func(name string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\ntitle: Fixture\ntype: note\n---\nbody\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("projects/local/note/local001.md")
	// Irrelevant siblings must be pruned before policy resolves their targets.
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "unrelated")); err != nil {
		t.Fatal(err)
	}
	foreign := "tenants/foreign/projects/p/note/foreign1.md"
	write(foreign)
	override := t.TempDir()
	if _, err := r.Provision(ctx, tenant.MustParse("external"), tenantfs.Overrides{BrainRoot: override, BlobRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(override, "external.md"), []byte("---\ntitle: Outside\n---\nprivate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(override, filepath.Join(dir, "override-alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "tenants"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	t.Run("scan", func(t *testing.T) {
		result, err := idx.IndexChanged()
		if err != nil {
			t.Fatal(err)
		}
		if result.Added != 1 {
			t.Errorf("scan admitted foreign files: added=%d, want 1", result.Added)
		}
	})
	t.Run("direct", func(t *testing.T) {
		for _, p := range []string{foreign, "alias/foreign/projects/p/note/foreign1.md", "override-alias/external.md"} {
			if err := idx.IndexFile(p); err == nil {
				t.Errorf("direct IndexFile admitted %s", p)
			}
		}
		if err := idx.IndexFile("projects/local/note/local001.md"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("health", func(t *testing.T) {
		if _, err := idx.GetHealth(); err != nil {
			t.Fatalf("unrelated sibling broke health: %v", err)
		}
	})
	t.Run("reconcile legacy contamination", func(t *testing.T) {
		if _, err := store.InsertNote(ctx, &storage.NoteRow{Path: foreign, ShortID: "foreign1", Title: "stale", Metadata: "{}"}); err != nil {
			t.Fatal(err)
		}
		if _, err := idx.IndexChanged(); err != nil {
			t.Fatal(err)
		}
		if row, err := store.GetNoteByPath(ctx, foreign); err != nil || row != nil {
			t.Fatalf("excluded local index row retained: %v %v", row, err)
		}
		if _, err := os.Stat(filepath.Join(dir, foreign)); err != nil {
			t.Fatalf("reconciliation changed foreign disk file: %v", err)
		}
		if _, err := idx.RebuildAll(); err != nil {
			t.Fatal(err)
		}
		if row, err := store.GetNoteByPath(ctx, foreign); err != nil || row != nil {
			t.Fatalf("rebuild ingested excluded file: %v %v", row, err)
		}
	})
	t.Run("watcher", func(t *testing.T) {
		fw, err := NewFileWatcher(dir, idx, &FileWatcherOptions{DebounceMs: 60000})
		if err != nil {
			t.Fatal(err)
		}
		if err := fw.Start(); err != nil {
			t.Fatal(err)
		}
		defer fw.Stop()
		watched := make(map[string]bool)
		for _, p := range fw.watcher.WatchList() {
			rel, _ := filepath.Rel(dir, p)
			watched[filepath.ToSlash(rel)] = true
			if rel == "tenants" {
				t.Error("initial walk watches foreign root")
			}
		}
		for _, want := range []string{".", "projects", "projects/local", "projects/local/note"} {
			if !watched[want] {
				t.Errorf("missing content/root watch: %s", want)
			}
		}
		write("tenants/new/projects/p/note/new00001.md")
		fw.handleEvent(fsnotify.Event{Name: filepath.Join(dir, "tenants/new"), Op: fsnotify.Create})
		fw.handleEvent(fsnotify.Event{Name: filepath.Join(dir, foreign), Op: fsnotify.Write})
		fw.mu.Lock()
		for p := range fw.pendingChanges {
			if p == foreign || p == "tenants/new/projects/p/note/new00001.md" {
				t.Errorf("foreign event queued: %s", p)
			}
		}
		// Flush must defend independently of admission at enqueue time.
		fw.pendingChanges["tenants/new/projects/p/note/new00001.md"] = "index"
		fw.mu.Unlock()
		fw.flushPendingChanges()
		row, err := store.GetNoteByPath(ctx, "tenants/new/projects/p/note/new00001.md")
		if err != nil {
			t.Fatal(err)
		}
		if row != nil {
			t.Error("flush indexed foreign note")
		}
	})
	t.Run("relevant dangling child remains fatal", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "projects/dangling.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := idx.IndexChanged(); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("content scan must fail: %v", err)
		}
		if _, err := idx.GetHealth(); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("content health must fail: %v", err)
		}
		fw, err := NewFileWatcher(dir, idx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer fw.Stop()
		if err := fw.Start(); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("content watch startup must fail: %v", err)
		}
	})
}
