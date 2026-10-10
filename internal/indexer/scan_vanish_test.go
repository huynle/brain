package indexer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/storage/storagetest"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

// Review follow-up O1 for brain task 0zr1gmn2: a file the scan listed can
// disappear before its per-file step (an out-of-band rm, or a REST delete/move
// in the gap between the service's os.Remove and RemoveFile taking the index
// lock). With the production tenantfs policy that used to abort the whole
// scan ("indexing failed, continuing with stale index"), skipping every later
// file and the final cleanup until the next restart.

// newVanishFixture returns an indexer over a fresh brain dir. With policy it
// is bound to the production local tenantfs root, as at boot.
func newVanishFixture(t *testing.T, policy bool) (string, *storage.TenantStore, *Indexer) {
	t.Helper()
	dir := t.TempDir()
	store, repo, err := storagetest.NewWithRegistry(t, filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	if !policy {
		return dir, store, NewIndexer(dir, store)
	}
	r, err := tenantfs.New(repo, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ProvisionLocal(context.Background(), dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return dir, store, NewIndexer(dir, store, r.Brain(tenant.Local))
}

func hasRow(t *testing.T, s *storage.TenantStore, rel string) bool {
	t.Helper()
	row, err := s.GetNoteByPath(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	return row != nil
}

// onStep runs fn once, inside the scan's critical section for path, before
// the step checks anything.
func onStep(idx *Indexer, path string, fn func()) *bool {
	fired := new(bool)
	idx.scanHook = func(stage scanStage, p string) {
		if stage == scanStageStep && p == path && !*fired {
			*fired = true
			fn()
		}
	}
	return fired
}

const (
	vanA     = "projects/p/note/a.md"
	vanB     = "projects/p/note/b.md"
	vanC     = "projects/p/note/c.md" // new on disk, after B in walk order
	vanStale = "projects/p/note/x.md" // indexed, removed before the scan
)

// seedVanish indexes A (and B when bIndexed) and a stale X, then adds C on
// disk only, so a completed scan must add C and delete X's row.
func seedVanish(t *testing.T, dir string, idx *Indexer, bIndexed bool) {
	t.Helper()
	writeBrainFile(t, dir, vanA, noteContent("a"))
	writeBrainFile(t, dir, vanB, noteContent("b"))
	writeBrainFile(t, dir, vanStale, noteContent("x"))
	for _, p := range []string{vanA, vanStale} {
		if err := idx.IndexFile(p); err != nil {
			t.Fatal(err)
		}
	}
	if bIndexed {
		if err := idx.IndexFile(vanB); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(dir, vanStale)); err != nil {
		t.Fatal(err)
	}
	writeBrainFile(t, dir, vanC, noteContent("c"))
}

// assertCompletedScan checks the scan ran to the end: later files indexed,
// the final cleanup ran, and the vanished file has no row.
func assertCompletedScan(t *testing.T, store *storage.TenantStore, result *IndexResult, err error, wantDeleted int) {
	t.Helper()
	if err != nil {
		t.Fatalf("scan aborted on a vanished file: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Errorf("vanished file reported as an error: %+v", result.Errors)
	}
	if !hasRow(t, store, vanC) {
		t.Error("later file C not indexed: scan stopped early")
	}
	if hasRow(t, store, vanStale) {
		t.Error("stale row X kept: final cleanup skipped")
	}
	if hasRow(t, store, vanB) {
		t.Error("vanished file B still has a row")
	}
	if !hasRow(t, store, vanA) {
		t.Error("unrelated row A lost")
	}
	if result.Added != 1 || result.Deleted != wantDeleted {
		t.Errorf("added=%d deleted=%d, want added=1 deleted=%d", result.Added, result.Deleted, wantDeleted)
	}
}

func TestIndexChanged_ListedFileVanishedBeforeStepIsTreatedAsGone(t *testing.T) {
	for _, policy := range []bool{true, false} {
		for _, bIndexed := range []bool{true, false} {
			name := map[bool]string{true: "policy", false: "nopolicy"}[policy] +
				map[bool]string{true: "/indexed", false: "/new"}[bIndexed]
			t.Run(name, func(t *testing.T) {
				dir, store, idx := newVanishFixture(t, policy)
				seedVanish(t, dir, idx, bIndexed)
				fired := onStep(idx, vanB, func() {
					// Out-of-band rm after the walk listed B.
					if err := os.Remove(filepath.Join(dir, vanB)); err != nil {
						t.Error(err)
					}
				})
				result, err := idx.IndexChanged()
				if !*fired {
					t.Fatal("hook never fired")
				}
				wantDeleted := 1 // X
				if bIndexed {
					wantDeleted = 2 // X and B's stale row
				}
				assertCompletedScan(t, store, result, err, wantDeleted)

				// The next scan is a no-op and does not resurrect B.
				idx.scanHook = nil
				again, err := idx.IndexChanged()
				if err != nil {
					t.Fatal(err)
				}
				if again.Added != 0 || again.Deleted != 0 || again.Updated != 0 || hasRow(t, store, vanB) {
					t.Errorf("follow-up scan not a no-op: %+v, B row=%v", again, hasRow(t, store, vanB))
				}
			})
		}
	}
}

// The service's delete and move remove the file first, then call RemoveFile,
// which may still be waiting for the index lock when the scan reaches the
// path. Reproduce that exact window with the production policy.
func TestIndexChanged_WriterDeleteOrMoveInRemoveGapNeverAbortsScan(t *testing.T) {
	const moved = "projects/q/note/b.md"
	for _, op := range []string{"delete", "move"} {
		t.Run(op, func(t *testing.T) {
			dir, store, idx := newVanishFixture(t, true)
			seedVanish(t, dir, idx, true)
			writer := make(chan error, 1)
			fired := onStep(idx, vanB, func() {
				if op == "move" {
					if err := writeBrainFileErr(dir, moved, noteContent("b moved")); err != nil {
						writer <- err
						return
					}
				}
				if err := os.Remove(filepath.Join(dir, vanB)); err != nil {
					writer <- err
					return
				}
				// RemoveFile (and IndexFile for a move) block on the lock
				// this step holds, exactly as in the service.
				go func() {
					if err := idx.RemoveFile(vanB); err != nil {
						writer <- err
						return
					}
					if op == "move" {
						writer <- idx.IndexFile(moved)
						return
					}
					writer <- nil
				}()
			})
			result, err := idx.IndexChanged()
			if !*fired {
				t.Fatal("hook never fired")
			}
			if werr := <-writer; werr != nil {
				t.Fatalf("writer: %v", werr)
			}
			assertCompletedScan(t, store, result, err, 2)
			if op == "move" && !hasRow(t, store, moved) {
				t.Error("moved entry has no row at its new path")
			}
		})
	}
}

// Only a confirmed vanish is forgiven. Any other per-file failure stays as
// visible as before: the scan returns the error (boot logs "indexing
// failed") and does not touch the file's row. This matches discovery, where a
// relevant dangling child or an unreadable directory is already fatal
// (TestTenantPolicyScanAndWatcher "relevant dangling child remains fatal").
func TestIndexChanged_RealPerFileErrorStillSurfaces(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	t.Run("permission", func(t *testing.T) {
		dir, store, idx := newVanishFixture(t, true)
		const locked = "projects/p/locked/b.md"
		writeBrainFile(t, dir, locked, noteContent("locked"))
		if err := idx.IndexFile(locked); err != nil {
			t.Fatal(err)
		}
		lockedDir := filepath.Join(dir, "projects/p/locked")
		t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })
		onStep(idx, locked, func() {
			if err := os.Chmod(lockedDir, 0); err != nil {
				t.Error(err)
			}
		})
		_, err := idx.IndexChanged()
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("permission failure not surfaced: %v", err)
		}
		_ = os.Chmod(lockedDir, 0o755)
		if !hasRow(t, store, locked) {
			t.Error("unreadable file's row deleted as if it had vanished")
		}
	})
	t.Run("dangling symlink", func(t *testing.T) {
		dir, store, idx := newVanishFixture(t, true)
		writeBrainFile(t, dir, vanB, noteContent("b"))
		if err := idx.IndexFile(vanB); err != nil {
			t.Fatal(err)
		}
		onStep(idx, vanB, func() {
			abs := filepath.Join(dir, vanB)
			if err := os.Remove(abs); err != nil {
				t.Error(err)
			}
			if err := os.Symlink(filepath.Join(dir, "missing.md"), abs); err != nil {
				t.Error(err)
			}
		})
		_, err := idx.IndexChanged()
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("dangling symlink not surfaced: %v", err)
		}
		if !hasRow(t, store, vanB) {
			t.Error("dangling symlink's row deleted as if the file had vanished")
		}
	})
}

// The discovery walk itself (the long phase on a large store) also sees
// entries vanish between reading a directory and visiting its children: a
// file, or a whole directory. Neither may abort discovery, scan or health.
func TestIndexChanged_EntriesVanishingDuringDiscoveryWalkNeverAbort(t *testing.T) {
	const (
		goneFile = "projects/p/note/b.md"
		goneDir  = "projects/p/zgone"
		inDir    = "projects/p/zgone/y.md"
		later    = "projects/q/note/z.md" // walked after both
	)
	for _, policy := range []bool{true, false} {
		t.Run(map[bool]string{true: "policy", false: "nopolicy"}[policy], func(t *testing.T) {
			dir, store, idx := newVanishFixture(t, policy)
			for _, p := range []string{vanA, goneFile, inDir, later} {
				writeBrainFile(t, dir, p, noteContent(p))
			}
			for _, p := range []string{goneFile, inDir} {
				if err := idx.IndexFile(p); err != nil {
					t.Fatal(err)
				}
			}
			fired := map[string]bool{}
			idx.scanHook = func(stage scanStage, p string) {
				if stage != scanStageWalk || fired[p] {
					return
				}
				switch p {
				case goneFile:
					fired[p] = true
					if err := os.Remove(filepath.Join(dir, goneFile)); err != nil {
						t.Error(err)
					}
				case goneDir:
					fired[p] = true
					if err := os.RemoveAll(filepath.Join(dir, goneDir)); err != nil {
						t.Error(err)
					}
				}
			}
			result, err := idx.IndexChanged()
			if err != nil {
				t.Fatalf("discovery aborted on a vanished entry: %v", err)
			}
			if !fired[goneFile] || !fired[goneDir] {
				t.Fatalf("hooks fired: %v", fired)
			}
			if len(result.Errors) != 0 {
				t.Errorf("vanished entries reported as errors: %+v", result.Errors)
			}
			for _, p := range []string{goneFile, inDir} {
				if hasRow(t, store, p) {
					t.Errorf("vanished %s still indexed", p)
				}
			}
			for _, p := range []string{vanA, later} {
				if !hasRow(t, store, p) {
					t.Errorf("%s not indexed: discovery stopped early", p)
				}
			}
		})
	}
}
