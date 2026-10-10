package indexer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/storage/storagetest"
)

// Brain task 0zr1gmn2: writers (Save/Update/Move/CheckoutFeature, the boot
// built-in Ensure* calls and the watcher) call IndexFile while the one-shot
// boot IndexChanged scan runs. The scan must never delete, overwrite or
// collide with a row a writer produced after the scan's snapshot.

// newSharedTestStorage uses one in-memory connection, matching production's
// SetMaxOpenConns(1), so concurrent goroutines see the same database (each
// ":memory:" connection would otherwise be a separate, empty database).
func newSharedTestStorage(t *testing.T) *storage.TenantStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := storagetest.NewWithDB(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func writeBrainFile(t *testing.T, brainDir, rel, content string) {
	t.Helper()
	if err := writeBrainFileErr(brainDir, rel, content); err != nil {
		t.Fatal(err)
	}
}

// writeBrainFileErr is safe to call from non-test goroutines.
func writeBrainFileErr(brainDir, rel, content string) error {
	full := filepath.Join(brainDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

func rawContent(t *testing.T, s *storage.TenantStore, rel string) string {
	t.Helper()
	row, err := s.GetNoteByPath(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	if row == nil {
		t.Fatalf("no index row for %s", rel)
	}
	if row.RawContent == nil {
		return ""
	}
	return *row.RawContent
}

// runWriterDuringHook starts write in a goroutine and waits for it for at most
// grace. A writer that is (correctly) serialized behind the scan's per-path
// critical section cannot finish inside the hook; the scan then proceeds and
// the writer runs afterwards. A writer that is not serialized finishes inside
// the hook, reproducing the unsynchronized interleaving deterministically.
func runWriterDuringHook(write func() error, grace time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() { done <- write() }()
	select {
	case err := <-done:
		replay := make(chan error, 1)
		replay <- err
		return replay
	case <-time.After(grace):
		return done
	}
}

// The production window: a writer indexes a brand-new file after the scan's
// disk walk already passed its directory, but before the scan snapshots the
// indexed rows. The row is in the DB snapshot and not in the disk snapshot.
func TestIndexChanged_KeepsRowIndexedByWriterAfterDiscovery(t *testing.T) {
	brainDir := createBrainDir(t, map[string]string{
		"projects/p/note/existing.md": noteContent("Existing"),
	})
	store := newSharedTestStorage(t)
	idx := NewIndexer(brainDir, store)

	const written = "global/automation/builtin1.md"
	idx.scanHook = func(stage scanStage, _ string) {
		if stage != scanStageDiscovered {
			return
		}
		writeBrainFile(t, brainDir, written, noteContent("Built-in written during scan"))
		if err := idx.IndexFile(written); err != nil {
			t.Errorf("writer IndexFile: %v", err)
		}
	}

	result, err := idx.IndexChanged()
	if err != nil {
		t.Fatalf("IndexChanged: %v", err)
	}
	if result.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0 (scan deleted a row a writer indexed during it)", result.Deleted)
	}
	if !strings.Contains(rawContent(t, store, written), "Built-in written during scan") {
		t.Fatalf("writer's row for %s lost", written)
	}
	// The next scan (next boot) must still keep it, as a normal on-disk file.
	idx.scanHook = nil
	if _, err := idx.IndexChanged(); err != nil {
		t.Fatal(err)
	}
	if countNotes(t, store) != 2 {
		t.Fatalf("notes = %d, want 2", countNotes(t, store))
	}
}

// A writer that rewrites and re-indexes a file while the scan holds an older
// parse of the same file must win: the scan may not write its stale parse
// over the writer's row.
func TestIndexChanged_WriterUpdateWinsOverStaleScanParse(t *testing.T) {
	const rel = "projects/p/note/target.md"
	brainDir := createBrainDir(t, map[string]string{rel: noteContent("v1")})
	store := newSharedTestStorage(t)
	idx := NewIndexer(brainDir, store)
	if err := idx.IndexFile(rel); err != nil {
		t.Fatal(err)
	}
	// Out-of-band edit: the scan will find a checksum mismatch and update.
	writeBrainFile(t, brainDir, rel, noteContent("v2-out-of-band"))

	var writer <-chan error
	idx.scanHook = func(stage scanStage, path string) {
		if stage != scanStageParsed || path != rel || writer != nil {
			return
		}
		writer = runWriterDuringHook(func() error {
			if err := writeBrainFileErr(brainDir, rel, noteContent("v3-writer")); err != nil {
				return err
			}
			return idx.IndexFile(rel)
		}, 300*time.Millisecond)
	}

	if _, err := idx.IndexChanged(); err != nil {
		t.Fatalf("IndexChanged: %v", err)
	}
	if writer == nil {
		t.Fatal("hook never fired")
	}
	if err := <-writer; err != nil {
		t.Fatalf("writer IndexFile: %v", err)
	}
	if got := rawContent(t, store, rel); !strings.Contains(got, "v3-writer") {
		t.Fatalf("row content = %q, want the writer's v3 (scan overwrote it with a stale parse)", got)
	}
}

// A file the scan discovered as new, which a writer indexes concurrently, must
// produce exactly one row and no "duplicate path" error on either side (the
// REST-save 500 seen in TestBuildHTTPHandler_DefaultLocalPWACompatibility).
func TestIndexChanged_ConcurrentInsertOfNewFileNoDuplicatePath(t *testing.T) {
	const rel = "projects/p/note/fresh.md"
	brainDir := createBrainDir(t, map[string]string{rel: noteContent("fresh")})
	store := newSharedTestStorage(t)
	idx := NewIndexer(brainDir, store)

	var writer <-chan error
	idx.scanHook = func(stage scanStage, path string) {
		if stage != scanStageParsed || path != rel || writer != nil {
			return
		}
		writer = runWriterDuringHook(func() error { return idx.IndexFile(rel) }, 300*time.Millisecond)
	}

	if _, err := idx.IndexChanged(); err != nil {
		t.Fatalf("IndexChanged: %v", err)
	}
	if writer == nil {
		t.Fatal("hook never fired")
	}
	if err := <-writer; err != nil {
		t.Fatalf("writer IndexFile: %v", err)
	}
	if n := countNotes(t, store); n != 1 {
		t.Fatalf("notes = %d, want 1", n)
	}
}

// A writer that removes a file (Move's old path, DELETE) after the scan's
// disk walk must not have the row resurrected by the scan.
func TestIndexChanged_WriterRemovalDuringScanIsNotResurrected(t *testing.T) {
	const rel = "projects/p/note/moved.md"
	brainDir := createBrainDir(t, map[string]string{
		rel:                     noteContent("old path"),
		"projects/p/note/a.md":  noteContent("a"),
		"projects/p/note/zz.md": noteContent("zz"),
	})
	store := newSharedTestStorage(t)
	idx := NewIndexer(brainDir, store)
	if err := idx.IndexFile(rel); err != nil {
		t.Fatal(err)
	}
	// Force the scan to rewrite rel from its (now stale) parse.
	writeBrainFile(t, brainDir, rel, noteContent("old path edited"))

	var writer <-chan error
	idx.scanHook = func(stage scanStage, path string) {
		if stage != scanStageParsed || path != rel || writer != nil {
			return
		}
		writer = runWriterDuringHook(func() error {
			if err := os.Remove(filepath.Join(brainDir, rel)); err != nil {
				return err
			}
			return idx.RemoveFile(rel)
		}, 300*time.Millisecond)
	}
	if _, err := idx.IndexChanged(); err != nil {
		t.Fatalf("IndexChanged: %v", err)
	}
	if writer == nil {
		t.Fatal("hook never fired")
	}
	if err := <-writer; err != nil {
		t.Fatalf("writer RemoveFile: %v", err)
	}
	row, err := store.GetNoteByPath(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	if row != nil {
		t.Fatalf("removed %s was resurrected by the scan", rel)
	}
}

// Stress: many writers create and index new files throughout a scan over a
// moderately large tree. Every written file keeps exactly one row and the scan
// never fails. Run under -race with a high -count.
func TestIndexChanged_ConcurrentWritersNeverLoseRows(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 300; i++ {
		files[fmt.Sprintf("projects/p/note/seed%04d.md", i)] = noteContent(fmt.Sprintf("seed %d", i))
	}
	brainDir := createBrainDir(t, files)
	store := newSharedTestStorage(t)
	idx := NewIndexer(brainDir, store)

	const writers, perWriter = 4, 15
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 4*writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			for i := 0; i < perWriter; i++ {
				// global/ sorts before projects/, so these land behind the walk.
				rel := fmt.Sprintf("global/automation/w%d-%02d.md", w, i)
				if err := writeBrainFileErr(brainDir, rel, noteContent(rel)); err != nil {
					errs <- err
					continue
				}
				if err := idx.IndexFile(rel); err != nil {
					errs <- fmt.Errorf("IndexFile %s: %w", rel, err)
				}
				// Also rewrite an existing seed, racing the scan's update path.
				seed := fmt.Sprintf("projects/p/note/seed%04d.md", (w*perWriter+i)%300)
				if err := writeBrainFileErr(brainDir, seed, noteContent("rewritten "+seed)); err != nil {
					errs <- err
					continue
				}
				if err := idx.IndexFile(seed); err != nil {
					errs <- fmt.Errorf("IndexFile %s: %w", seed, err)
				}
			}
		}(w)
	}
	var once sync.Once
	idx.scanHook = func(stage scanStage, _ string) {
		if stage == scanStageDiscovered {
			once.Do(func() { close(start) })
		}
	}
	result, err := idx.IndexChanged()
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if err != nil {
		t.Fatalf("IndexChanged: %v", err)
	}
	if result.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0", result.Deleted)
	}
	want := 300 + writers*perWriter
	if n := countNotes(t, store); n != want {
		t.Fatalf("notes = %d, want %d (rows lost or duplicated)", n, want)
	}
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			seed := fmt.Sprintf("projects/p/note/seed%04d.md", (w*perWriter+i)%300)
			if got := rawContent(t, store, seed); !strings.Contains(got, "rewritten "+seed) {
				t.Errorf("%s row = %q, want the writer's rewrite (stale scan parse won)", seed, got)
			}
		}
	}
}
